package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/rag"
)

const findToolsToolName = "find_tools"

var dynamicFindToolsSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Natural language description of the capability or tool you need."
			},
			"num_results": {
				"type": "integer",
				"minimum": 1,
				"maximum": 20,
				"description": "Maximum number of tools to return."
			}
		},
		"required": ["query"],
		"additionalProperties": false
	}`)

func buildDynamicSessionTools(toolset *types.Toolset, vectorToolStore *rag.ToolsetVectorStore) []*toolListEntry {
	toolsetName := ""
	toolsetDescription := ""
	if toolset != nil {
		toolsetName = toolset.Name
		toolsetDescription = conv.PtrValOrEmpty(toolset.Description, "")
	}
	contextDescription := toolsetName
	if contextDescription != "" && toolsetDescription != "" {
		contextDescription = fmt.Sprintf("%s (%s)", toolsetName, toolsetDescription)
	}
	if contextDescription == "" && toolsetDescription != "" {
		contextDescription = toolsetDescription
	}

	findDescription := "Search the available tools in this MCP server using a search query. The result will be tools that could help you complete your task"
	executeDescription := "Execute a specific tool by name, passing through the correct arguments for that tool's schema."
	if contextDescription != "" {
		findDescription = fmt.Sprintf("Search the available tools in %s using a search query. The result will be tools to help you complete your task", contextDescription)
		executeDescription = fmt.Sprintf("Execute a specific tool from %s by name, passing through the correct arguments for that tool's schema.", contextDescription)
	}

	return []*toolListEntry{
		{
			Name:        findToolsToolName,
			Description: findDescription,
			InputSchema: dynamicFindToolsSchema,
		},
		{
			Name:        executeToolToolName,
			Description: executeDescription,
			InputSchema: dynamicExecuteToolSchema,
		},
	}
}

type findToolsArguments struct {
	Query      string `json:"query"`
	NumResults int    `json:"num_results"`
}

func handleFindToolsCall(
	ctx context.Context,
	logger *slog.Logger,
	reqID msgID,
	argsRaw json.RawMessage,
	toolset *types.Toolset,
	vectorToolStore *rag.ToolsetVectorStore,
) (json.RawMessage, error) {
	indexed, err := vectorToolStore.ToolsetToolsAreIndexed(ctx, *toolset)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check toolset indexing status").Log(ctx, logger)
	}
	// TODO: Since we're currently in experimental mode, this index will be updated as needed.
	// The long-term goal is to migrate this into an executable Temporal workflow.
	// Once a toolset can be marked as dynamically indexed, we can trigger asynchronous actions
	// to automatically update indexes whenever toolsets or deployments change.
	if !indexed {
		if indexErr := vectorToolStore.IndexToolset(ctx, *toolset); indexErr != nil {
			return nil, oops.E(oops.CodeUnexpected, indexErr, "failed to prepare tool search index").Log(ctx, logger)
		}
	}

	var args findToolsArguments
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse find_tools arguments").Log(ctx, logger)
		}
	}

	query := strings.TrimSpace(args.Query)
	if query == "" {
		return nil, oops.E(oops.CodeInvalid, errors.New("missing query"), "query is required").Log(ctx, logger)
	}

	searchResults, err := vectorToolStore.SearchToolsetTools(ctx, *toolset, query, args.NumResults)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to search tools").Log(ctx, logger)
	}

	// Build a map of tools by name for quick lookup
	toolsByName := make(map[string]*types.Tool)
	for _, tool := range toolset.Tools {
		baseTool := conv.ToBaseTool(tool)
		toolsByName[baseTool.Name] = tool
	}

	// constuct full tool entries with similarity scores
	var results []*toolListEntry
	for _, searchResult := range searchResults {
		tool, exists := toolsByName[searchResult.ToolName]
		if !exists {
			continue
		}

		name, description, inputSchema, meta := conv.ToToolListEntry(tool)
		if name == "" {
			continue
		}

		// Add similarity score to meta
		if meta == nil {
			meta = make(map[string]any)
		}
		meta["similarity_score"] = searchResult.SimilarityScore

		results = append(results, &toolListEntry{
			Name:        name,
			Description: description,
			InputSchema: inputSchema,
			Meta:        meta,
		})
	}

	payload, err := json.Marshal(toolsListResult{Tools: results})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tool search result").Log(ctx, logger)
	}

	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     string(payload),
		MimeType: nil,
		Data:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tool search chunk").Log(ctx, logger)
	}

	response, err := json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content: []json.RawMessage{chunk},
			IsError: false,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize find_tools response").Log(ctx, logger)
	}

	return response, nil
}

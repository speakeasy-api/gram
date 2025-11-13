package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	temporal_client "go.temporal.io/sdk/client"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/background"
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
	findDescription := "Search through the available tools in this MCP server using a search query. The result will be a list of tools that could help you complete your task."
	executeDescription := "Execute a specific tool by name, passing through the correct arguments for that tool's schema."
	tree, _ := buildToolTree(toolset.Tools)
	if len(tree) > 0 {
		var toolAreas []string
		for source, group := range tree {
			if group == nil {
				for tag := range group.groups {
					if tag == "default" {
						toolAreas = append(toolAreas, source)
					} else {
						toolAreas = append(toolAreas, fmt.Sprintf("%s/%s", source, tag))
					}
				}
			}
		}
		findDescription += fmt.Sprintf(" The available tools in this server relate to these areas: %s.", strings.Join(toolAreas, ", "))
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
	temporal temporal_client.Client,
) (json.RawMessage, error) {
	indexed, err := vectorToolStore.ToolsetToolsAreIndexed(ctx, *toolset)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to check toolset indexing status").Log(ctx, logger)
	}

	if !indexed {
		wr, indexErr := background.ExecuteIndexToolset(
			ctx,
			temporal,
			background.IndexToolsetParams{
				ProjectID:   uuid.MustParse(toolset.ProjectID),
				ToolsetSlug: toolset.Slug,
			},
		)

		if indexErr != nil {
			return nil, oops.E(oops.CodeUnexpected, indexErr, "failed to prepare tool search index").Log(ctx, logger)
		}

		wrError := wr.Get(ctx, nil)
		if wrError != nil {
			return nil, oops.E(oops.CodeUnexpected, wrError, "failed to build tool search index").Log(ctx, logger)

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

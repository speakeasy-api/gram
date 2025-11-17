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
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/rag"
)

const (
	searchToolsToolName    = "search_tools"
	NEEDS_SEARCH_THRESHOLD = 30
)

var dynamicSearchToolsSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Natural language description of the capability or tool you need."
			},
			"num_results": {
				"type": "integer",
				"minimum": 1,
				"maximum": 10,
				"description": "Maximum number of tools to return."
			}
		},
		"required": ["query"],
		"additionalProperties": false
	}`)

func buildDynamicSessionTools(toolset *types.Toolset, vectorToolStore *rag.ToolsetVectorStore) []*toolListEntry {
	findDescription := "Search through the available tools in this MCP server using a search query. The result will be a list of tools that could help you complete your task."
	executeDescription := "Execute a specific tool by name, passing through the correct arguments for that tool's schema. Do not call a tool without first describing it to get the input schema."
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

	searchToolRequired := len(toolset.Tools) > NEEDS_SEARCH_THRESHOLD

	tools := []*toolListEntry{}
	if searchToolRequired {
		tools = append(tools, &toolListEntry{
			Name:        searchToolsToolName,
			Description: findDescription,
			InputSchema: dynamicSearchToolsSchema,
		})
	}
	tools = append(tools, buildDescribeToolsTool(toolset.Tools, searchToolRequired))
	tools = append(tools, &toolListEntry{
		Name:        executeToolToolName,
		Description: executeDescription,
		InputSchema: dynamicExecuteToolSchema,
	})

	return tools
}

func buildDescribeToolsTool(tools []*types.Tool, searchToolRequired bool) *toolListEntry {
	description := "Describe a set of tools by name. Use this to get more information about a tool, such as its description and input schema."

	// For smaller toolsets, we just list the available tools in this tool's description and omit the search tool
	if searchToolRequired {
		description += fmt.Sprintf(" You can find what tools are available using the %s tool.", searchToolsToolName)
	} else {
		toolNames := []string{}
		for _, tool := range tools {
			baseTool := conv.ToBaseTool(tool)
			toolNames = append(toolNames, baseTool.Name)
		}
		description += fmt.Sprintf(" The available tools are: %s.", strings.Join(toolNames, ", "))
	}

	schemaJSON := `{
		"type": "object",
		"properties": {
			"tool_names": {
				"type": "array",
				"items": {
					"type": "string",
					"description": "Exact name of the tool to describe. Example: 'get_github_repo'"
				},
				"description": "Names of the tools to describe."
			}
		},
		"required": ["tool_names"],
		"additionalProperties": false
	}`
	return &toolListEntry{
		Name:        describeToolsToolName,
		Description: description,
		InputSchema: json.RawMessage(schemaJSON),
		Meta:        nil,
	}
}

type searchToolsArguments struct {
	Query      string `json:"query"`
	NumResults int    `json:"num_results"`
}

func handleSearchToolsCall(
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

	var args searchToolsArguments
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse search_tools arguments").Log(ctx, logger)
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

type describeToolsArguments struct {
	ToolNames []string `json:"tool_names"`
}

func handleDescribeToolsCall(
	ctx context.Context,
	logger *slog.Logger,
	reqID msgID,
	argsRaw json.RawMessage,
	toolset *types.Toolset,
) (json.RawMessage, error) {
	var args describeToolsArguments
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse describe_tools arguments").Log(ctx, logger)
		}
	}

	if len(args.ToolNames) == 0 {
		return nil, oops.E(oops.CodeInvalid, errors.New("missing tool_names"), "tool_names are required").Log(ctx, logger)
	}

	// Build a map of tools by name for quick lookup
	toolsByName := make(map[string]*types.Tool)
	for _, tool := range toolset.Tools {
		baseTool := conv.ToBaseTool(tool)
		toolsByName[baseTool.Name] = tool
	}

	// Find requested tools
	entries := make([]*toolListEntry, 0, len(args.ToolNames))
	notFound := make([]string, 0)
	for _, name := range args.ToolNames {
		name = strings.TrimSpace(name)
		if tool, exists := toolsByName[name]; exists {
			if entry := toolToListEntry(tool); entry != nil {
				entries = append(entries, entry)
			}
		} else {
			notFound = append(notFound, name)
		}
	}

	if len(notFound) > 0 {
		logger.WarnContext(ctx, "some tools not found", attr.SlogExpected(notFound))
	}

	payload, err := json.Marshal(toolsListResult{Tools: entries})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tool description result").Log(ctx, logger)
	}

	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     string(payload),
		MimeType: nil,
		Data:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tool description chunk").Log(ctx, logger)
	}

	response, err := json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content: []json.RawMessage{chunk},
			IsError: false,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize describe_tools response").Log(ctx, logger)
	}

	return response, nil
}

type sourceGroup struct {
	groups map[string][]*types.Tool
}

func buildToolTree(tools []*types.Tool) (map[string]*sourceGroup, error) {
	tree := make(map[string]*sourceGroup)

	for _, tool := range tools {
		toolURN, err := conv.GetToolURN(*tool)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get tool urn")
		}

		source := toolURN.Source
		group := "default"

		if tool.HTTPToolDefinition != nil {
			tags := tool.HTTPToolDefinition.Tags
			if len(tags) > 0 {
				group = tags[0]
			}
		}

		if tree[source] == nil {
			tree[source] = &sourceGroup{
				groups: make(map[string][]*types.Tool),
			}
		}

		tree[source].groups[group] = append(tree[source].groups[group], tool)
	}

	return tree, nil
}

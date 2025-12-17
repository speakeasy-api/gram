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
	searchToolsToolName = "search_tools"
)

func buildDynamicSearchToolsSchema(availableTags []string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Natural language description of the capability or tool you need."
			},
			"tags": {
				"type": "array",
				"items": {
					"type": "string",
					"description": "Tag to filter the results by."
				},
				"description": "Tags to filter the results by. If not provided, all tools will be returned. Available tags: %s"
			},
			"match_mode": {
				"type": "string",
				"enum": ["any", "all"],
				"default": "any",
				"description": "How to match the tags, if provided. If 'any', the results will be tools that match any of the tags. If 'all', the results will be tools that match all of the tags."
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
	}`, strings.Join(availableTags, ", ")))
}

func buildDynamicSessionTools(
	ctx context.Context,
	logger *slog.Logger,
	toolset *types.Toolset,
	vectorToolStore *rag.ToolsetVectorStore,
	temporal temporal_client.Client,
) ([]*toolListEntry, error) {
	if err := waitForIndexing(ctx, logger, toolset, vectorToolStore, temporal); err != nil {
		return nil, fmt.Errorf("failed to index toolset: %w", err)
	}

	findDescription := "Search through the available tools in this MCP server using a search query. The result will be a list of tools that could help you complete your task."
	executeDescription := "Execute a specific tool by name, passing through the correct arguments for that tool's schema. Do not call a tool without first describing it to get the input schema."

	availableTags, _ := vectorToolStore.GetToolsetAvailableTags(ctx, *toolset)
	if len(availableTags) > 0 {
		findDescription += fmt.Sprintf(" The available tools fall under the following categories: %s.", strings.Join(availableTags, ", "))
	}

	describeToolsTool, err := buildDescribeToolsTool(toolset.Tools)
	if err != nil {
		return nil, err
	}

	return []*toolListEntry{
		{
			Name:        searchToolsToolName,
			Description: findDescription,
			InputSchema: buildDynamicSearchToolsSchema(availableTags),
			Meta:        nil,
		},
		describeToolsTool,
		{
			Name:        executeToolToolName,
			Description: executeDescription,
			InputSchema: dynamicExecuteToolSchema,
			Meta:        nil,
		},
	}, nil
}

func buildDescribeToolsTool(tools []*types.Tool) (*toolListEntry, error) {
	toolNames := []string{}
	for _, tool := range tools {
		if conv.IsProxyTool(tool) {
			return nil, fmt.Errorf("build describe tools with external mcp proxy: %s", tool.ExternalMcpToolDefinition.Name)
		}

		baseTool, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
		toolNames = append(toolNames, baseTool.Name)
	}

	description := "Describe a set of tools by name. Use this to get more information about a tool, such as its description and input schema."
	description += fmt.Sprintf(" You can find what tools are available using the %s tool.", searchToolsToolName)

	exampleCount := min(len(toolNames), 3)
	schemaJSON := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"tool_names": {
				"type": "array",
				"items": {
					"type": "string",
					"description": "Exact name of the tool to describe. Examples: %s"
				},
				"description": "Names of the tools to describe."
			}
		},
		"required": ["tool_names"],
		"additionalProperties": false
	}`, strings.Join(toolNames[:exampleCount], ", "))

	return &toolListEntry{
		Name:        describeToolsToolName,
		Description: description,
		InputSchema: json.RawMessage(schemaJSON),
		Meta:        nil,
	}, nil
}

type searchToolsArguments struct {
	Query      string        `json:"query"`
	Tags       []string      `json:"tags"`
	MatchMode  rag.MatchMode `json:"match_mode"`
	NumResults int           `json:"num_results"`
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
	if err := waitForIndexing(ctx, logger, toolset, vectorToolStore, temporal); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to index toolset").Log(ctx, logger)
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

	searchOptions := rag.SearchToolsOptions{
		Query:     query,
		Tags:      args.Tags,
		MatchMode: rag.MatchModeAny,
		Limit:     args.NumResults,
	}

	searchResults, err := vectorToolStore.SearchToolsetTools(ctx, *toolset, searchOptions)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to search tools").Log(ctx, logger)
	}

	// Build a map of tools by name for quick lookup
	toolsByName := make(map[string]*types.Tool)
	for _, tool := range toolset.Tools {
		if conv.IsProxyTool(tool) {
			return nil, fmt.Errorf("search tools with external mcp proxy: %s", tool.ExternalMcpToolDefinition.Name)
		}

		baseTool, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
		toolsByName[baseTool.Name] = tool
	}

	// construct full tool entries with similarity scores
	var results []*toolListEntry
	for _, searchResult := range searchResults {
		tool, exists := toolsByName[searchResult.ToolName]
		if !exists {
			continue
		}

		toolEntry, err := conv.ToToolListEntry(tool)
		if err != nil {
			continue
		}
		if toolEntry.Name == "" {
			continue
		}

		// Add similarity score and tags to meta
		meta := toolEntry.Meta
		if meta == nil {
			meta = make(map[string]any)
		}
		meta["similarity_score"] = searchResult.SimilarityScore
		meta["tags"] = searchResult.Tags

		results = append(results, &toolListEntry{
			Name:        toolEntry.Name,
			Description: toolEntry.Description,
			Meta:        meta,
			InputSchema: nil, // Intentional don't return to keep token usage down
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
		Meta:     nil,
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

func waitForIndexing(ctx context.Context, logger *slog.Logger, toolset *types.Toolset, vectorToolStore *rag.ToolsetVectorStore, temporal temporal_client.Client) error {
	indexed, err := vectorToolStore.ToolsetToolsAreIndexed(ctx, *toolset)
	if err != nil {
		return fmt.Errorf("failed to check toolset indexing status: %w", err)
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
			return fmt.Errorf("failed to prepare tool search index: %w", indexErr)
		}

		wrError := wr.Get(ctx, nil)
		if wrError != nil {
			return fmt.Errorf("failed to build tool search index: %w", wrError)

		}
	}

	return nil
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
		if conv.IsProxyTool(tool) {
			return nil, fmt.Errorf("describe tools with external mcp proxy: %s", tool.ExternalMcpToolDefinition.Name)
		}

		baseTool, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}
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
		Meta:     nil,
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

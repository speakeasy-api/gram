package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

type toolsListResult struct {
	Tools []*toolListEntry `json:"tools"`
}

type toolListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty,omitzero"`
	Meta        map[string]any  `json:"_meta,omitempty"`
}

func handleToolsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest, productMetrics *posthog.Posthog, toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents], vectorStore *toolVectorStore) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), toolsetCache)
	if err != nil {
		return nil, err
	}

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		if err := productMetrics.CaptureEvent(ctx, "mcp_server_count", payload.sessionID, map[string]interface{}{
			"project_id":           payload.projectID.String(),
			"organization_id":      toolset.OrganizationID,
			"authenticated":        payload.authenticated,
			"toolset":              toolset.Name,
			"toolset_slug":         toolset.Slug,
			"toolset_id":           toolset.ID,
			"mcp_domain":           requestContext.Host,
			"mcp_url":              requestContext.Host + requestContext.ReqURL,
			"mcp_enabled":          toolset.McpEnabled,
			"disable_notification": true,
			"mcp_session_id":       payload.sessionID,
		}); err != nil {
			logger.ErrorContext(ctx, "failed to capture mcp_server_count event", attr.SlogError(err))
		}
	}

	tools := buildToolListEntries(toolset.Tools)

	if payload.isDynamicMCPSession {
		if err := vectorStore.IndexToolset(ctx, toolset.ID, tools); err != nil {
			logger.ErrorContext(ctx, "failed to index toolset for vector search", attr.SlogError(err))
		}

		tools = buildDynamicSessionTools(toolset)
	}

	result := &result[toolsListResult]{
		ID: req.ID,
		Result: toolsListResult{
			Tools: tools,
		},
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").Log(ctx, logger)
	}

	return bs, nil
}

func buildToolListEntries(tools []*types.Tool) []*toolListEntry {
	result := make([]*toolListEntry, 0, len(tools))
	for _, tool := range tools {
		if entry := toolToListEntry(tool); entry != nil {
			result = append(result, entry)
		}
	}
	return result
}

func toolToListEntry(tool *types.Tool) *toolListEntry {
	if tool == nil {
		return nil
	}

	var meta map[string]any
	if tool.FunctionToolDefinition != nil {
		meta = tool.FunctionToolDefinition.Meta
	}

	baseTool := conv.ToBaseTool(tool)

	return &toolListEntry{
		Name:        baseTool.Name,
		Description: baseTool.Description,
		InputSchema: json.RawMessage(baseTool.Schema),
		Meta:        meta,
	}
}

func buildDynamicSessionTools(toolset *types.Toolset) []*toolListEntry {
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

	findDescription := "Search the available tools in this MCP server using a search query."
	executeDescription := "Execute a specific tool by name, passing through the correct arguments for that tool's schema."
	if contextDescription != "" {
		findDescription = fmt.Sprintf("Search the available tools in %s using a search query.", contextDescription)
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

var (
	dynamicFindToolsSchema = json.RawMessage(`{
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

	dynamicExecuteToolSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Exact name of the tool to execute."
			},
			"arguments": {
				"description": "JSON payload to forward to the tool as its arguments."
			}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)
)

package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/rag"
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

func handleToolsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest, productMetrics *posthog.Posthog, toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents], vectorToolStore *rag.ToolsetVectorStore) (json.RawMessage, error) {
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

	var tools []*toolListEntry
	switch payload.mode {
	case ToolModeDynamic:
		tools = buildDynamicSessionTools(toolset, vectorToolStore)
	case ToolModeStatic:
		fallthrough
	default:
		tools = buildToolListEntries(toolset.Tools)
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

	name, description, inputSchema, meta := conv.ToToolListEntry(tool)

	return &toolListEntry{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Meta:        meta,
	}
}

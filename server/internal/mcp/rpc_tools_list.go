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
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	temporal_client "go.temporal.io/sdk/client"
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

func handleToolsList(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	payload *mcpInputs,
	req *rawRequest,
	productMetrics *posthog.Posthog,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	vectorToolStore *rag.ToolsetVectorStore,
	temporal temporal_client.Client,
) (json.RawMessage, error) {
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
		tools, err = buildDynamicSessionTools(ctx, logger, toolset, vectorToolStore, temporal)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to build dynamic session tools").Log(ctx, logger)
		}
	case ToolModeStatic:
		fallthrough
	default:
		tools = buildToolListEntries(toolset.Tools)

		// Unfold proxy tools from external MCP servers
		unfoldedTools, err := unfoldExternalMCPTools(ctx, logger, toolset.Tools, payload.oauthTokenInputs)
		if err != nil {
			logger.WarnContext(ctx, "failed to unfold external MCP tools", attr.SlogError(err))
			// Continue with regular tools even if unfolding fails
		} else {
			tools = append(tools, unfoldedTools...)
		}
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

func unfoldExternalMCPTools(ctx context.Context, logger *slog.Logger, tools []*types.Tool, tokenInputs []oauthTokenInputs) ([]*toolListEntry, error) {
	var oauthToken string
	for _, t := range tokenInputs {
		if len(t.securityKeys) == 0 && t.Token != "" {
			oauthToken = t.Token
			break
		}
	}

	var result []*toolListEntry

	for _, tool := range tools {
		if !conv.IsProxyTool(tool) {
			continue
		}

		externalMcpTool := tool.ExternalMcpToolDefinition

		var opts *externalmcp.ClientOptions
		if externalMcpTool.RequiresOauth && oauthToken != "" {
			opts = &externalmcp.ClientOptions{
				Authorization: "Bearer " + oauthToken,
				TransportType: externalmcptypes.TransportType(externalMcpTool.TransportType),
			}
		}

		mcpClient, err := externalmcp.NewClient(ctx, logger, externalMcpTool.RemoteURL, externalmcptypes.TransportType(externalMcpTool.TransportType), opts)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to connect to external MCP").Log(ctx, logger)
		}
		externalTools, err := mcpClient.ListTools(ctx)
		if closeErr := mcpClient.Close(); closeErr != nil {
			return nil, oops.E(oops.CodeUnexpected, closeErr, "failed to close external MCP client").Log(ctx, logger)
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools from external MCP").Log(ctx, logger)
		}

		for _, extTool := range externalTools {
			result = append(result, &toolListEntry{
				Name:        externalMcpTool.Slug + "--" + extTool.Name,
				Description: extTool.Description,
				InputSchema: extTool.Schema,
				Meta:        nil,
			})
		}
	}

	return result, nil
}

func toolToListEntry(tool *types.Tool) *toolListEntry {
	if tool == nil {
		return nil
	}

	// Skip proxy tools - they are handled separately via unfoldExternalMCPTools
	if conv.IsProxyTool(tool) {
		return nil
	}

	toolEntry, err := conv.ToToolListEntry(tool)
	if err != nil {
		return nil
	}

	return &toolListEntry{
		Name:        toolEntry.Name,
		Description: toolEntry.Description,
		InputSchema: toolEntry.InputSchema,
		Meta:        toolEntry.Meta,
	}
}

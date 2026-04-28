package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type toolsListResult = result[toolsListResultTools]

type toolsListResultTools struct {
	Tools []*toolListEntry `json:"tools"`
}

type toolListEntry struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	InputSchema json.RawMessage              `json:"inputSchema,omitempty,omitzero"`
	Annotations *externalmcp.ToolAnnotations `json:"annotations,omitempty"`
	Meta        map[string]any               `json:"_meta,omitempty"`
}

func handleToolsList(
	ctx context.Context,
	logger *slog.Logger,
	authzEngine *authz.Engine,
	guardianPolicy *guardian.Policy,
	db *pgxpool.Pool,
	env toolconfig.EnvironmentLoader,
	payload *mcpInputs,
	req *rawRequest,
	productMetrics *posthog.Posthog,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	vectorToolStore *rag.ToolsetVectorStore,
	temporalEnv *temporal.Environment,
	features *productfeatures.Client,
) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), toolsetCache)
	if err != nil {
		return nil, err
	}

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		if err := productMetrics.CaptureEvent(ctx, "mcp_server_count", payload.sessionID, map[string]any{
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
		tools, err = buildDynamicSessionTools(ctx, logger, toolset, vectorToolStore, temporalEnv)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to build dynamic session tools").Log(ctx, logger)
		}
	case ToolModeStatic:
		fallthrough
	default:
		tools, err = buildToolListEntries(ctx, logger, guardianPolicy, db, env, payload, toolset)
		if err != nil {
			return nil, err
		}
	}

	// Filter tools by RBAC grants. Private authenticated MCPs enforce
	// per-tool mcp:connect checks — the same dimensions used by tools/call.
	// Public MCPs skip this (open to everyone, matching the connection guard).
	if payload.authenticated && authzEngine != nil && (toolset.McpIsPublic == nil || !*toolset.McpIsPublic) {
		tools, err = filterToolsByAuthz(ctx, logger, authzEngine, toolset.ID, tools)
		if err != nil {
			return nil, err
		}
	}

	if blockShadowMCPEnabled(ctx, logger, features, toolset.OrganizationID) {
		for _, t := range tools {
			injected, err := injectToolsetIDConstant(t.InputSchema, toolset.ID)
			if err != nil {
				logger.WarnContext(ctx, "failed to inject toolset id constant into tool input schema", attr.SlogError(err), attr.SlogToolName(t.Name))
				continue
			}
			t.InputSchema = injected
		}
	}

	result := &toolsListResult{
		ID: req.ID,
		Result: toolsListResultTools{
			Tools: tools,
		},
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").Log(ctx, logger)
	}

	return bs, nil
}

func buildToolListEntries(
	ctx context.Context,
	logger *slog.Logger,
	guardianPolicy *guardian.Policy,
	db *pgxpool.Pool,
	envLoader toolconfig.EnvironmentLoader,
	payload *mcpInputs,
	toolset *types.Toolset,
) ([]*toolListEntry, error) {
	toolsetHelpers := toolsets.NewToolsets(db)

	toolsetID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse toolset ID").Log(ctx, logger)
	}

	userConfig := toolconfig.CIEnvFrom(payload.mcpEnvVariables)

	// Extract general OAuth token (no security-key binding)
	var oauthToken string
	for _, t := range payload.oauthTokenInputs {
		if len(t.securityKeys) == 0 && t.Token != "" {
			oauthToken = t.Token
			break
		}
	}

	var tools []*toolListEntry

	executor := externalmcp.BuildProxyToolExecutor(logger, guardianPolicy, toolset.Tools)
	if executor.HasEntries() {
		resolve := func(ctx context.Context, toolURN urn.Tool, projectID uuid.UUID) (*externalmcp.ToolCallPlan, error) {
			plan, err := toolsetHelpers.GetToolCallPlanByURN(ctx, toolURN, projectID)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to get tool call plan by URN")
			}
			if plan.Kind != gateway.ToolKindExternalMCP || plan.ExternalMCP == nil {
				return nil, oops.E(oops.CodeUnexpected, nil, "expected external MCP plan for proxy tool")
			}
			return plan.ExternalMCP, nil
		}

		loadSystemEnv := func(ctx context.Context, toolURN urn.Tool) (*toolconfig.CaseInsensitiveEnv, error) {
			return envLoader.LoadSystemEnv(ctx, payload.projectID, toolsetID, string(toolURN.Kind), toolURN.Source)
		}

		proxyTools, err := executor.DoList(ctx, payload.projectID, userConfig, oauthToken, loadSystemEnv, resolve)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list proxy tools").Log(ctx, logger)
		}

		for _, extTool := range proxyTools {
			tools = append(tools, &toolListEntry{
				Name:        extTool.Name,
				Description: extTool.Description,
				InputSchema: extTool.Schema,
				Annotations: extTool.Annotations,
				Meta:        nil,
			})
		}
	}
	for _, tool := range toolset.Tools {
		if !conv.IsProxyTool(tool) {
			if entry := toolToListEntry(tool); entry != nil {
				tools = append(tools, entry)
			}
		}
	}

	return tools, nil
}

func toolToListEntry(tool *types.Tool) *toolListEntry {
	if tool == nil {
		return nil
	}

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
		Annotations: convertConvAnnotations(toolEntry.Annotations),
		Meta:        toolEntry.Meta,
	}
}

// filterToolsByAuthz removes tools the caller is not authorized to invoke.
// Each tool is checked against mcp:connect with a tool-name dimension, matching
// the per-tool RBAC check performed by tools/call.
func filterToolsByAuthz(ctx context.Context, logger *slog.Logger, engine *authz.Engine, toolsetID string, tools []*toolListEntry) ([]*toolListEntry, error) {
	allowed := make([]*toolListEntry, 0, len(tools))
	for _, t := range tools {
		ok, err := engine.IsAllowed(ctx, authz.MCPToolCallCheck(toolsetID, authz.MCPToolCallDimensions{
			Tool: t.Name,
		}))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check tool-level authz for tools/list").Log(ctx, logger)
		}
		if ok {
			allowed = append(allowed, t)
		}
	}
	return allowed, nil
}

// convertConvAnnotations converts conv.ToolAnnotations to externalmcp.ToolAnnotations.
func convertConvAnnotations(c *conv.ToolAnnotations) *externalmcp.ToolAnnotations {
	if c == nil {
		return nil
	}
	return &externalmcp.ToolAnnotations{
		Title:           c.Title,
		ReadOnlyHint:    c.ReadOnlyHint,
		DestructiveHint: c.DestructiveHint,
		IdempotentHint:  c.IdempotentHint,
		OpenWorldHint:   c.OpenWorldHint,
	}
}

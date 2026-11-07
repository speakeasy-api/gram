package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

type toolsListResult = result[toolsListResultTools]

type toolsListResultTools struct {
	Tools []*toolListEntry `json:"tools"`
}

type toolListEntry struct {
	Title        string                       `json:"title,omitempty"`
	OutputSchema json.RawMessage              `json:"outputSchema,omitempty"`
	Icons        json.RawMessage              `json:"icons,omitempty"`
	Execution    json.RawMessage              `json:"execution,omitempty"`
	Name         string                       `json:"name"`
	Description  string                       `json:"description"`
	InputSchema  json.RawMessage              `json:"inputSchema,omitempty,omitzero"`
	Annotations  *externalmcp.ToolAnnotations `json:"annotations,omitempty"`
	Meta         map[string]any               `json:"_meta,omitempty"`
}

func handleToolsList(
	ctx context.Context,
	logger *slog.Logger,
	authzEngine *authz.Engine,
	db *pgxpool.Pool,
	payload *mcpInputs,
	req *rawRequest,
	productMetrics *posthog.Posthog,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	vectorToolStore *rag.ToolsetVectorStore,
	shadowMCPClient *shadowmcp.Client,
	platformExtras []platformtools.ExternalTool,
	clientInfoStore sessionClientInfoStore,
) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), toolsetCache, payload.toolVariationsGroupID, platformExtras...)
	if err != nil {
		return nil, err
	}

	// Apply the ?tags= filter before building any tool list (static or
	// dynamic). Filtering toolset.Tools here also restricts dynamic-mode
	// search_tools/describe_tools, which resolve results back through this same
	// slice.
	if len(payload.tags) > 0 {
		before := len(toolset.Tools)
		toolset.Tools = toolfilter.FilterToolsByTags(toolset.Tools, payload.tags)
		recordToolFilterSpan(ctx, len(toolset.Tools), before-len(toolset.Tools))
	}

	// The consent-screen tool selection narrows the same slice the ?tags=
	// filter does, so static mode, dynamic-mode search/describe, and the
	// call-side lookup all agree on the session's visible tools.
	if payload.toolSelection != nil {
		before := len(toolset.Tools)
		toolset.Tools = toolfilter.FilterToolsBySelection(toolset.Tools, payload.toolSelection)
		recordToolFilterSpan(ctx, len(toolset.Tools), before-len(toolset.Tools))
	}

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		// Identity and protocol-version parity with `mcp_initialized`: under
		// 2026-07-28 there is no initialize to capture them at, so this event
		// carries them per request. For handshake-era clients the identity
		// resolution below falls back to the Redis-backed initialize record —
		// a deliberate read on the tools/list path so those clients stay
		// attributable in PostHog, accepted because it only runs when the
		// event is actually emitted. The `_meta` decode is equally scoped to
		// the event: tools/list params are tiny, so the scan is negligible.
		reqMeta := mcprequests.ParseMeta(req.Params)
		identity, storedProtocolVersion := resolveClientIdentity(ctx, logger, clientInfoStore, payload, reqMeta.ClientInfo)
		protocolVersion := payload.protocolVersion.Declared
		if protocolVersion == "" {
			protocolVersion = storedProtocolVersion
		}

		props := map[string]any{
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
			"protocol_version":     protocolVersion,
			"client_name":          conv.PtrEmpty(identity.Name),
			"client_version":       conv.PtrEmpty(identity.Version),
			"capabilities":         reqMeta.CapabilityKeys,
		}
		payload.recordServingAnalytics(props)
		// Preserve existing analytics consumers while also emitting the shared
		// hosted/remote event. Consumers should select one event, not sum both.
		for _, event := range []string{"mcp_server_count", "mcp_server_tools_list"} {
			if err := productMetrics.CaptureEvent(ctx, event, payload.sessionID, props); err != nil {
				logger.ErrorContext(ctx, "failed to capture "+event+" event", attr.SlogError(err))
			}
		}
	}

	// A restrictive session whose effective selection is empty gets no
	// dynamic facade either: search_tools would still reach the embedding
	// search service, a surface a zero-tool grant does not cover.
	mode := payload.mode
	if payload.toolSelection != nil && len(toolset.Tools) == 0 {
		mode = ToolModeStatic
	}

	var tools []*toolListEntry
	switch mode {
	case ToolModeDynamic:
		tools, err = buildDynamicSessionTools(ctx, logger, toolset, vectorToolStore)
		if err != nil {
			if errors.Is(err, errToolSearchIndexUnavailable) {
				return nil, oops.E(oops.CodeUnavailable, err, "tool search is temporarily unavailable; try again later").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "failed to build dynamic session tools").LogError(ctx, logger)
		}
	case ToolModeStatic:
		fallthrough
	default:
		tools = buildToolListEntries(toolset)
	}

	// Filter tools by RBAC grants. Private authenticated MCPs enforce
	// per-tool mcp:connect checks — the same dimensions used by tools/call.
	// Public MCPs skip this (open to everyone, matching the connection guard).
	// Both the privacy read and the resource id follow the wrapper when one
	// fronts the request.
	if payload.authenticated && authzEngine != nil && payload.effectiveMCPPrivate(toolset.McpIsPublic) {
		tools, err = filterToolsByMCPConnect(ctx, logger, authzEngine, payload.mcpConnectResourceID(toolset.ID), payload.projectID.String(), tools)
		if err != nil {
			return nil, err
		}
	}

	toolsetProjectID, err := uuid.Parse(toolset.ProjectID)
	if err != nil {
		logger.WarnContext(ctx, "invalid toolset project id; skipping shadow_mcp schema injection", attr.SlogError(err))
	} else if shadowMCPClient.IsEnabledForProject(ctx, toolsetProjectID) {
		for _, t := range tools {
			injected, err := shadowmcp.InjectToolsetIDConstant(t.InputSchema, toolset.ID)
			if err != nil {
				logger.WarnContext(ctx, "failed to inject toolset id constant into tool input schema", attr.SlogError(err), attr.SlogToolName(t.Name))
				continue
			}
			t.InputSchema = injected
		}
	}

	// An empty toolset must list [] rather than null: some clients reject a
	// null tools array during startup.
	if tools == nil {
		tools = []*toolListEntry{}
	}

	result := &toolsListResult{
		ID: req.ID,
		Result: toolsListResultTools{
			Tools: tools,
		},
		serverIdentity: serverInfoHostedToolset,
		// Caller-varying unconditionally, on four independent axes: the
		// per-tool authorization filter above, the Gram-Mode header selecting
		// static or dynamic tools, the session's consent-screen tool
		// selection, and the ?tags= filter. Only the first is gated on server
		// visibility, so a public server is no more shareable than a private
		// one.
		cacheHints: cacheHintsCallerVarying,
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").LogError(ctx, logger)
	}

	return bs, nil
}

func buildToolListEntries(toolset *types.Toolset) []*toolListEntry {
	var tools []*toolListEntry
	for _, tool := range toolset.Tools {
		if entry := toolToListEntry(tool); entry != nil {
			tools = append(tools, entry)
		}
	}

	return tools
}

func toolToListEntry(tool *types.Tool) *toolListEntry {
	if tool == nil {
		return nil
	}

	toolEntry, err := conv.ToToolListEntry(tool)
	if err != nil {
		return nil
	}

	return &toolListEntry{
		Title: "", OutputSchema: nil, Icons: nil, Execution: nil,
		Name:        toolEntry.Name,
		Description: toolEntry.Description,
		InputSchema: toolEntry.InputSchema,
		Annotations: convertConvAnnotations(toolEntry.Annotations),
		Meta:        toolEntry.Meta,
	}
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

// dispositionFromAnnotations derives a disposition string from tool annotations.
func dispositionFromAnnotations(a *externalmcp.ToolAnnotations) string {
	if a == nil {
		return ""
	}
	switch {
	case a.ReadOnlyHint != nil && *a.ReadOnlyHint:
		return "read_only"
	case a.DestructiveHint != nil && *a.DestructiveHint:
		return "destructive"
	case a.IdempotentHint != nil && *a.IdempotentHint:
		return "idempotent"
	case a.OpenWorldHint != nil && *a.OpenWorldHint:
		return "open_world"
	default:
		return ""
	}
}

// filterToolsByMCPConnect keeps the tools the caller may connect to and stops
// once the request is canceled.
func filterToolsByMCPConnect(ctx context.Context, logger *slog.Logger, authzEngine *authz.Engine, resourceID string, projectID string, tools []*toolListEntry) ([]*toolListEntry, error) {
	allowed := make([]*toolListEntry, 0, len(tools))
	for _, t := range tools {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("filter tools by authz: %w", err)
		}
		if err := authzEngine.Require(ctx, authz.MCPToolCallCheck(resourceID, authz.MCPToolCallDimensions{
			Tool:        t.Name,
			Disposition: dispositionFromAnnotations(t.Annotations),
			ProjectID:   projectID,
		})); err != nil {
			var oopsErr *oops.ShareableError
			if errors.As(err, &oopsErr) && oopsErr.Code == oops.CodeForbidden {
				continue
			}
			return nil, oops.E(oops.CodeUnexpected, err, "check tool-level authz for tools/list").LogError(ctx, logger)
		}
		allowed = append(allowed, t)
	}
	return allowed, nil
}

package remotemcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ToolsListMCPConnectFilterInterceptor drops tools the caller is not
// authorized for via the per-tool `mcp:connect` RBAC dimension. It
// mirrors the per-tool refinement that [ToolsCallAuthzInterceptor]
// enforces on tools/call, applied here at tools/list response time so
// the caller never sees a tool they couldn't invoke.
//
// Attached only for private-visibility servers, matching
// [ToolsCallAuthzInterceptor]'s gate (see [Service.buildProxy]). Public
// servers bypass server-level RBAC by design — filtering the catalog
// would be a no-op against grants that don't constrain the caller.
//
// Each per-tool check carries the `disposition` dimension, resolved from
// admin-authored tool metadata via the injected [ToolDispositionResolver] so
// the filter matches disposition-scoped grants the same way the paired
// tools/call enforcement does. A tool with no recorded metadata resolves to
// the empty disposition, leaving a pure tool-name match.
type ToolsListMCPConnectFilterInterceptor struct {
	authz       *authz.Engine
	resolver    ToolDispositionResolver
	mcpServerID string
	projectID   string
	logger      *slog.Logger
}

var _ proxy.ToolsListResponseInterceptor = (*ToolsListMCPConnectFilterInterceptor)(nil)

// NewToolsListMCPConnectFilterInterceptor constructs an interceptor
// scoped to a single Remote MCP Server. mcpServerID is the [authz.Check]
// ResourceID, the mcp_servers row id (NOT the remote_mcp_servers id), so
// the filter resolves grants against the same mcp_servers row that the
// handler's upfront server-level `mcp:connect` check uses, the same shape
// [authz.MCPToolCallCheck] uses for the paired tools/call enforcement.
func NewToolsListMCPConnectFilterInterceptor(authzEngine *authz.Engine, resolver ToolDispositionResolver, mcpServerID, projectID string, logger *slog.Logger) *ToolsListMCPConnectFilterInterceptor {
	return &ToolsListMCPConnectFilterInterceptor{
		authz:       authzEngine,
		resolver:    resolver,
		mcpServerID: mcpServerID,
		projectID:   projectID,
		logger:      logger,
	}
}

// Name implements [proxy.ToolsListResponseInterceptor].
func (i *ToolsListMCPConnectFilterInterceptor) Name() string {
	return "tools-list-mcp-connect-filter"
}

// InterceptToolsListResponse implements [proxy.ToolsListResponseInterceptor].
// It builds one [authz.MCPToolCallCheck] per tool, hands the batch to
// [authz.Engine.FindMatched] for per-tool match indicators (one
// challenge-log entry for the batch, not N), and rebuilds the tool
// slice in input order keeping only authorized entries.
//
// When the response carries no tools the interceptor is a no-op. An
// empty filtered result is a valid outcome — the caller has access to
// nothing in this server — and is committed via [SetTools] as an empty
// array.
func (i *ToolsListMCPConnectFilterInterceptor) InterceptToolsListResponse(ctx context.Context, list *proxy.ToolsListResponse) error {
	if i.authz == nil || list == nil || list.Result == nil {
		return nil
	}
	tools := list.Result.Tools
	if len(tools) == 0 {
		return nil
	}

	// Fail closed: if disposition resolution fails, surface the error rather
	// than filtering on the empty disposition, which would leak tools an
	// annotation-scoped grant is meant to withhold. One lookup covers the
	// whole batch (the resolver caches the server's full tool set).
	dispositions, err := i.resolver.Dispositions(ctx, i.mcpServerID, i.projectID)
	if err != nil {
		return fmt.Errorf("resolve remote MCP tool dispositions: %w", err)
	}

	checks := make([]authz.Check, len(tools))
	for idx, t := range tools {
		checks[idx] = authz.MCPToolCallCheck(i.mcpServerID, authz.MCPToolCallDimensions{
			Tool:        t.Name,
			Disposition: dispositions[t.Name],
			ProjectID:   i.projectID,
		})
	}

	matched, err := i.authz.FindMatched(ctx, checks)
	if err != nil {
		return fmt.Errorf("filter mcp:connect tools: %w", err)
	}

	allowed := make([]*mcp.Tool, 0, len(tools))
	for idx, t := range tools {
		if matched[idx] {
			allowed = append(allowed, t)
		}
	}

	if err := list.SetTools(allowed); err != nil {
		return fmt.Errorf("commit filtered tools/list result: %w", err)
	}
	return nil
}

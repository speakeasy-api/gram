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
// authorized for via the per-tool `mcp:connect` RBAC dimension, mirroring at
// tools/list response time the refinement [ToolsCallAuthzInterceptor] enforces
// on tools/call, so the caller never sees a tool they couldn't invoke.
//
// Attached only for private-visibility servers, matching
// [ToolsCallAuthzInterceptor]'s gate in [ProxyManager.BuildTarget]. Public
// servers bypass server-level RBAC by design, so filtering the catalog would
// be a no-op against grants that don't constrain the caller.
//
// Each per-tool check carries the `disposition` dimension, resolved from
// admin-authored tool metadata via the injected [ToolDispositionResolver] so
// the filter matches disposition-scoped grants the same way the paired
// tools/call enforcement does. A tool with no recorded metadata resolves to
// the empty disposition, leaving a pure tool-name match.
//
// The filter leaves cache labelling to the proxy, which marks every tools/list
// result relayed through a chain with this filter attached caller-varying with
// a zero ttl, whether or not anything was filtered.
//
// A 2xx tools/list whose result does not decode as [mcp.ListToolsResult]
// never reaches the typed interceptor loop at all, and unless the proxy's
// StrictToolSelection is set (only when a consent selection is attached) such
// a response relays unfiltered. That gap predates this interceptor, so
// closing it belongs with the strict-handling gate.
//
// A catalog that survives intact is not rewritten, so its tools member keeps
// its original values. A filtering rewrite replaces the tools member,
// re-marshaling kept tools through [mcp.Tool] and dropping per-tool members
// the SDK does not model. Every other member of the upstream result relays
// untouched, including members future protocol revisions add. Should such a
// member ever carry tool identities the way tools does, this filter will not
// scrub it.
type ToolsListMCPConnectFilterInterceptor struct {
	authz       *authz.Engine
	resolver    ToolDispositionResolver
	mcpServerID string
	projectID   string
	logger      *slog.Logger
}

var _ proxy.ToolsListResponseInterceptor = (*ToolsListMCPConnectFilterInterceptor)(nil)

// NewToolsListMCPConnectFilterInterceptor constructs an interceptor scoped to
// a single Remote MCP Server. mcpServerID is the [authz.Check] ResourceID, the
// mcp_servers row id (NOT the remote_mcp_servers id), so the filter resolves
// grants against the same row as the handler's upfront server-level
// `mcp:connect` check and as [authz.MCPToolCallCheck] does for the paired
// tools/call enforcement.
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
// [authz.Engine.FindMatched] for per-tool match indicators (one challenge-log
// entry for the batch, not N), and rebuilds the tool slice in input order
// keeping only authorized entries.
//
// A response carrying a JSON-RPC error rather than a result is left alone: it
// holds no inventory to filter. An empty filtered result is a valid outcome,
// meaning the caller can reach nothing in this server, and commits via
// [proxy.ToolsListResponse.SetTools] as an empty array.
func (i *ToolsListMCPConnectFilterInterceptor) InterceptToolsListResponse(ctx context.Context, list *proxy.ToolsListResponse) error {
	if list == nil || list.Result == nil {
		return nil
	}
	if i.authz == nil {
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

	// Replacing an unchanged catalog is not free: SetTools re-marshals every
	// kept tool through mcp.Tool, dropping per-tool members the SDK does not
	// model. Leave it alone so the tools member keeps its original values.
	if len(allowed) == len(tools) {
		return nil
	}

	if err := list.SetTools(allowed); err != nil {
		return fmt.Errorf("commit filtered tools/list result: %w", err)
	}
	return nil
}

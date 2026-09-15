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
// Every response this interceptor sees is labelled caller-varying, whether or
// not filtering removed anything: a catalog that survived intact is still the
// product of this caller's grants, and is the widest such catalog. Labelling
// on attachment rather than on effect also keeps the label from becoming an
// oracle for whether the caller was filtered, and keeps one logical listing
// from taking a split stance across pages.
//
// "Sees" is the limit of that guarantee. A 2xx tools/list whose result does
// not decode as [mcp.ListToolsResult] never reaches the typed interceptor loop
// at all, and unless the proxy's StrictToolSelection is set (only when a
// consent selection is attached) such a response relays unfiltered and
// unlabelled. That gap predates this interceptor and skips the filtering too,
// so closing it belongs with the strict-handling gate.
//
// An unfiltered rewrite touches only the two caching members, leaving the
// tools member's original wire bytes; a filtering rewrite also replaces the
// tools member, re-marshaling kept tools through [mcp.Tool] and dropping
// per-tool members the SDK does not model. Every other member of the upstream
// result relays untouched, including members future protocol revisions add.
// Should such a member ever carry tool identities the way tools does, this
// filter will not scrub it.
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
// holds no inventory to filter or label. Every other response is labelled
// caller-varying before returning, including the ones with no filtering left
// to do, since an upstream that offered no tools and a nil engine both still
// describe what this caller may reach. An empty filtered result is a valid
// outcome, meaning the caller can reach nothing in this server, and commits
// via [proxy.ToolsListResponse.SetPrivateTools] as an empty array.
func (i *ToolsListMCPConnectFilterInterceptor) InterceptToolsListResponse(ctx context.Context, list *proxy.ToolsListResponse) error {
	if list == nil || list.Result == nil {
		return nil
	}
	if i.authz == nil {
		return markCallerVarying(list)
	}
	tools := list.Result.Tools
	if len(tools) == 0 {
		return markCallerVarying(list)
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

	// Replacing an unchanged catalog is not free: SetPrivateTools re-marshals
	// every kept tool through mcp.Tool, dropping per-tool members the SDK does
	// not model. Label instead and let the original bytes relay.
	if len(allowed) == len(tools) {
		return markCallerVarying(list)
	}

	if err := list.SetPrivateTools(allowed); err != nil {
		return fmt.Errorf("commit filtered tools/list result: %w", err)
	}
	return nil
}

// markCallerVarying labels a tools/list result this filter left intact.
// Failing here is an internal invariant break, not a caller error: relaying
// the result unlabelled would defeat the point of the filter, so the error
// propagates.
func markCallerVarying(list *proxy.ToolsListResponse) error {
	if err := list.MarkCallerVarying(); err != nil {
		return fmt.Errorf("label unfiltered tools/list result caller-varying: %w", err)
	}
	return nil
}

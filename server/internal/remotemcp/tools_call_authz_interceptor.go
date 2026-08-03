package remotemcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ToolsCallAuthzInterceptor enforces the per-tool `mcp:connect` RBAC dimension
// check for tools/call invocations against a Remote MCP Server. It is a
// [proxy.ToolsCallRequestInterceptor]: it runs after the generic user-request
// chain and before the request is forwarded upstream. The handler enforces
// the server-level `mcp:connect` grant for private-visibility servers ahead
// of the proxy (see [Service.serveRemoteBackend]); this interceptor is the
// finer per-tool refinement and is therefore only attached for private
// visibility. Public servers bypass server-level RBAC by design, so per-tool
// RBAC is also skipped — see the conditional attach in [Service.buildProxy].
//
// The check carries both the tool-name and `disposition` dimensions. The
// disposition is resolved from admin-authored tool metadata via the injected
// [ToolDispositionResolver]; a tool with no recorded metadata resolves to the
// empty disposition, leaving the check as a pure tool-name match.
type ToolsCallAuthzInterceptor struct {
	authz       *authz.Engine
	resolver    ToolDispositionResolver
	mcpServerID string
	projectID   string
	logger      *slog.Logger
}

var _ proxy.ToolsCallRequestInterceptor = (*ToolsCallAuthzInterceptor)(nil)

// NewToolsCallAuthzInterceptor constructs an interceptor scoped to a single
// Remote MCP Server. Callers build a fresh instance per request so the
// resource ID for the authz check is captured without re-parsing routing
// state. mcpServerID is the mcp_servers row id (NOT the remote_mcp_servers
// id) so the per-tool `mcp:connect` check resolves grants against the same
// resource id the handler's upfront server-level `mcp:connect` check uses,
// keeping per-tool and server-level authorization consistent. projectID is
// the owning project for the mcp_endpoint and is forwarded as a dimension so
// project-scoped grants can match.
func NewToolsCallAuthzInterceptor(authzEngine *authz.Engine, resolver ToolDispositionResolver, mcpServerID, projectID string, logger *slog.Logger) *ToolsCallAuthzInterceptor {
	return &ToolsCallAuthzInterceptor{
		authz:       authzEngine,
		resolver:    resolver,
		mcpServerID: mcpServerID,
		projectID:   projectID,
		logger:      logger,
	}
}

// Name implements [proxy.ToolsCallRequestInterceptor].
func (i *ToolsCallAuthzInterceptor) Name() string {
	return "tools-call-authz"
}

// InterceptToolsCallRequest implements [proxy.ToolsCallRequestInterceptor].
// It runs the dimensioned `mcp:connect` Require call against the configured
// server ID and the tool name from the request params. Any error from the
// engine — forbidden or otherwise — is surfaced to the user as the JSON-RPC
// rejection envelope; the caller does not silently filter on tools/call (the
// silent-omission semantics are reserved for tools/list parity, which is
// gated on payload mutation support).
//
// [authz.MCPToolCallCheck] names its first parameter `toolsetID` for legacy
// reasons, but the helper only stores it as the opaque `ResourceID` on the
// returned [authz.Check]; passing the mcp_servers UUID is semantically
// correct. It is the same resource id the handler runs the upfront
// server-level `mcp:connect` check against, so per-tool and server-level
// enforcement agree on what they are authorizing.
func (i *ToolsCallAuthzInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	// Defensive: the proxy's typed dispatch (toolsCallRequestFromUserRequest)
	// only constructs a ToolsCallRequest with non-nil Params, so this branch
	// is unreachable in practice. The guard exists so direct callers (tests,
	// future programmatic use) are safe.
	if i.authz == nil || call == nil || call.Params == nil {
		return nil
	}

	// Fail closed: a resolution failure rejects the call rather than falling
	// back to the empty disposition, which would relax an annotation-scoped
	// grant exactly when the metadata store is unavailable. This is an internal
	// failure, not a permission denial, so it surfaces as-is (any interceptor
	// error aborts the call) rather than through the "you lack access" message.
	dispositions, err := i.resolver.Dispositions(ctx, i.mcpServerID, i.projectID)
	if err != nil {
		return fmt.Errorf("resolve remote MCP tool disposition: %w", err)
	}

	err = i.authz.Require(ctx, authz.MCPToolCallCheck(i.mcpServerID, authz.MCPToolCallDimensions{
		Tool:        call.Params.Name,
		Disposition: dispositions[call.Params.Name],
		ProjectID:   i.projectID,
	}))
	if err != nil {
		return fmt.Errorf("authorize remote MCP tool call: %w", mcpaccess.ToolPermissionDenied(err))
	}

	return nil
}

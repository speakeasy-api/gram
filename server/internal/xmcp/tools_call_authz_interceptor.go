package xmcp

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ToolsCallAuthzInterceptor enforces the per-tool `mcp:connect` RBAC dimension
// check for tools/call invocations against a Remote MCP Server. It is a
// [proxy.ToolsCallRequestInterceptor]: it runs after the generic user-request
// chain and before the request is forwarded upstream. The handler already
// enforces the server-level `mcp:connect` grant ahead of the proxy (see
// [Service.ServeMCP]), so this interceptor is the finer per-tool refinement.
//
// Only the tool-name dimension is checked here. Disposition awareness depends
// on the per-session tools/list response cache tracked separately, and is
// added when that cache lands.
type ToolsCallAuthzInterceptor struct {
	authz    *authz.Engine
	serverID string
	logger   *slog.Logger
}

var _ proxy.ToolsCallRequestInterceptor = (*ToolsCallAuthzInterceptor)(nil)

// NewToolsCallAuthzInterceptor constructs an interceptor scoped to a single
// Remote MCP Server. Callers build a fresh instance per request so the
// resource ID for the authz check is captured without re-parsing routing
// state.
func NewToolsCallAuthzInterceptor(authzEngine *authz.Engine, serverID string, logger *slog.Logger) *ToolsCallAuthzInterceptor {
	return &ToolsCallAuthzInterceptor{
		authz:    authzEngine,
		serverID: serverID,
		logger:   logger,
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
// returned [authz.Check]; passing a Remote MCP Server UUID is semantically
// correct and matches how the same scope is enforced for the server-level
// check at [Service.ServeMCP].
func (i *ToolsCallAuthzInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	// Defensive: the proxy's typed dispatch (toolsCallRequestFromUserRequest)
	// only constructs a ToolsCallRequest with non-nil Params, so this branch
	// is unreachable in practice. The guard exists so direct callers (tests,
	// future programmatic use) are safe.
	if i.authz == nil || call == nil || call.Params == nil {
		return nil
	}

	return i.authz.Require(ctx, authz.MCPToolCallCheck(i.serverID, authz.MCPToolCallDimensions{
		Tool:        call.Params.Name,
		Disposition: "",
	}))
}

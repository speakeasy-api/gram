package remotemcp

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ToolsCallOTELCounterInterceptor records the per-tool MCP call counter for each
// tools/call invocation against a Remote MCP Server. It is a
// [proxy.UserRequestInterceptor] so parseable calls with malformed method params
// are still counted before typed request decoding. This mirrors `/mcp` (which
// records before forwarding to the upstream tool executor) so the same metric
// tracks attempted calls regardless of upstream success.
type ToolsCallOTELCounterInterceptor struct {
	metrics  *ProxyMetrics
	identity proxy.ServerIdentity
	logger   *slog.Logger
}

var _ proxy.UserRequestInterceptor = (*ToolsCallOTELCounterInterceptor)(nil)

// NewToolsCallOTELCounterInterceptor constructs an interceptor bound to the
// given metrics object and server correlation ids. Construct one per request
// so the counter's `gram.remote_mcp_server.id` and `gram.mcp_server.id`
// labels are closed over without re-deriving them from the URL path on every
// call.
func NewToolsCallOTELCounterInterceptor(m *ProxyMetrics, identity proxy.ServerIdentity, logger *slog.Logger) *ToolsCallOTELCounterInterceptor {
	return &ToolsCallOTELCounterInterceptor{
		metrics:  m,
		identity: identity,
		logger:   logger,
	}
}

// Name implements [proxy.UserRequestInterceptor].
func (i *ToolsCallOTELCounterInterceptor) Name() string {
	return "tools-call-otel-counter"
}

// InterceptUserRequest implements [proxy.UserRequestInterceptor]. Always
// returns nil — counter recording is best-effort and must not block tool
// invocation or take ownership of method-parameter validation.
func (i *ToolsCallOTELCounterInterceptor) InterceptUserRequest(ctx context.Context, req *proxy.UserRequest) error {
	if i == nil || i.metrics == nil {
		return nil
	}
	toolName, isToolsCall := proxy.ToolsCallName(req)
	if !isToolsCall {
		return nil
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil
	}

	var mcpURL string
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		mcpURL = requestContext.Host + requestContext.ReqURL
	}

	i.metrics.RecordMCPToolCall(ctx, authCtx.ActiveOrganizationID, mcpURL, i.identity, toolName)
	return nil
}

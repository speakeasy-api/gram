package xmcp

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// ToolsCallOTELCounterInterceptor records the per-tool MCP call counter for each
// tools/call invocation against a Remote MCP Server. It is a
// [proxy.ToolsCallRequestInterceptor]: counting at the request side mirrors
// `/mcp` (which records before forwarding to the upstream tool executor) so
// the same metric tracks attempted calls regardless of upstream success.
type ToolsCallOTELCounterInterceptor struct {
	metrics *metrics
	logger  *slog.Logger
}

var _ proxy.ToolsCallRequestInterceptor = (*ToolsCallOTELCounterInterceptor)(nil)

// NewToolsCallOTELCounterInterceptor constructs an interceptor bound to the given
// xmcp metrics object. The same instance can be reused across requests.
func NewToolsCallOTELCounterInterceptor(m *metrics, logger *slog.Logger) *ToolsCallOTELCounterInterceptor {
	return &ToolsCallOTELCounterInterceptor{
		metrics: m,
		logger:  logger,
	}
}

// Name implements [proxy.ToolsCallRequestInterceptor].
func (i *ToolsCallOTELCounterInterceptor) Name() string {
	return "tools-call-otel-counter"
}

// InterceptToolsCallRequest implements [proxy.ToolsCallRequestInterceptor].
// Always returns nil — counter recording is best-effort and must not block
// tool invocation on metrics-backend failures.
func (i *ToolsCallOTELCounterInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	if i.metrics == nil || call == nil || call.Params == nil {
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

	i.metrics.RecordMCPToolCall(ctx, authCtx.ActiveOrganizationID, mcpURL, call.Params.Name)
	return nil
}

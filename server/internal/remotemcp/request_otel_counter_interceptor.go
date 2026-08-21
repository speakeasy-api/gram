package remotemcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// RequestOTELCounterInterceptor records the per-request MCP census (`mcp.request`)
// for the remote- and tunnel-backed `/x/mcp` traffic, which bypasses the mcp
// package's JSON-RPC dispatch where the hosted and platform surfaces emit the
// same instrument. It is a [proxy.UserRequestInterceptor] so it observes every
// parsed inbound message regardless of method, after routing and
// authentication — matching the dispatch-site semantics on the other surfaces.
//
// The declared protocol version comes from the MCP-Protocol-Version header,
// falling back to the 2026-07-28 per-request `_meta` key the header mirrors.
// Both surfaces the proxy serves face third-party clients, so everything here
// records as [mcpmetrics.SurfaceHosting].
type RequestOTELCounterInterceptor struct {
	metrics *mcpmetrics.RequestCounter
}

var _ proxy.UserRequestInterceptor = (*RequestOTELCounterInterceptor)(nil)

// NewRequestOTELCounterInterceptor constructs the census interceptor. It holds no
// per-request state, so one instance is shared across all proxied requests.
func NewRequestOTELCounterInterceptor(metrics *mcpmetrics.RequestCounter) *RequestOTELCounterInterceptor {
	return &RequestOTELCounterInterceptor{metrics: metrics}
}

// Name implements [proxy.UserRequestInterceptor].
func (i *RequestOTELCounterInterceptor) Name() string {
	return "request-otel-counter"
}

// InterceptUserRequest implements [proxy.UserRequestInterceptor]. Always
// returns nil — the census is observation-only and must never reject a
// request.
func (i *RequestOTELCounterInterceptor) InterceptUserRequest(ctx context.Context, req *proxy.UserRequest) error {
	if req == nil {
		return nil
	}

	var headerVersion string
	if req.UserHTTPRequest != nil {
		headerVersion = req.UserHTTPRequest.Header.Get(mcpversions.HTTPHeader)
	}

	for _, msg := range req.JSONRPCMessages {
		rpcReq, ok := msg.(*jsonrpc.Request)
		if !ok {
			continue
		}

		i.metrics.Record(ctx, mcprequests.DeclaredProtocolVersion(headerVersion, rpcReq.Params), rpcReq.Method, mcpmetrics.SurfaceHosting)
	}

	return nil
}

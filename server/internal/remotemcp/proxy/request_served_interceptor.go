package proxy

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// RequestServedInterceptor is told once that the remote MCP server answered a
// request: a 2xx reply carrying the JSON-RPC response to it, relayed to the
// user after every interceptor accepted it. The response may still be an MCP
// error, a JSON-RPC error or an isError result; [ResponseSucceeded] tells. It
// only observes, and runs on the request's goroutine, so it must not block.
//
// It does not run for notifications, for initialize, for requests refused or
// rejected on the way, for a tools/call reply the typed response chain did not
// recognise, or for replies that never reached the user.
type RequestServedInterceptor interface {
	// InterceptRequestServed is called once the response is relayed.
	InterceptRequestServed(ctx context.Context, response *jsonrpc.Response)

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

package proxy

import (
	"context"
)

// RequestServedInterceptor is told once that the remote MCP server answered a
// request: a 2xx reply carrying the JSON-RPC response to it, relayed to the
// user after every interceptor accepted it. A JSON-RPC error response counts:
// the server answered. It only observes, and runs on the request's goroutine,
// so it must not block.
//
// It does not run for notifications, for requests refused or rejected on the
// way, or for replies that never reached the user.
type RequestServedInterceptor interface {
	// InterceptRequestServed is called once the response is relayed.
	InterceptRequestServed(ctx context.Context)

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

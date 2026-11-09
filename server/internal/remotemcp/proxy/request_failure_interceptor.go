package proxy

import (
	"context"
	"net/http"
)

// RequestFailure describes a forwarded request other than "tools/call", such
// as "initialize" or "tools/list", that the remote MCP server answered with a
// non-2xx HTTP status.
type RequestFailure struct {
	// Method is the JSON-RPC method, empty for a forwarded JSON-RPC response.
	Method string

	// StatusCode is the remote MCP server's HTTP status.
	StatusCode int

	// Header is the remote MCP server's response header.
	Header http.Header
}

// RequestFailureInterceptor is told about each [RequestFailure] before the
// response is relayed. It only observes: the user receives the same response
// either way. A failed "tools/call" goes to [ToolsCallFailureInterceptor]
// instead.
type RequestFailureInterceptor interface {
	// InterceptRequestFailure is called with the failed request.
	InterceptRequestFailure(ctx context.Context, failure *RequestFailure)

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

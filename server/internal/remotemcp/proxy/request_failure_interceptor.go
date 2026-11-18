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

	// SentHeader is the request header that reached the server, after any
	// redirect or retry.
	SentHeader http.Header

	// Err is the error the proxy returned to its caller, such as a response
	// interceptor's, or nil when the response was relayed.
	Err error
}

// RequestFailureInterceptor is told about each [RequestFailure] once the
// proxy is done with the request. It only observes: the user's answer is
// already decided. A failed "tools/call" goes to [ToolsCallFailureInterceptor]
// instead.
type RequestFailureInterceptor interface {
	// InterceptRequestFailure is called with the failed request.
	InterceptRequestFailure(ctx context.Context, failure *RequestFailure)

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

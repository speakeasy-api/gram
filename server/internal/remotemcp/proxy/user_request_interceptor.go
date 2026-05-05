package proxy

import "context"

// UserRequestInterceptor runs for each inbound user request after it has been
// parsed but before it is forwarded to the remote MCP server.
//
// The current contract is inspection and rejection: implementations may
// observe req and return a non-nil error to reject the request before it
// reaches the remote server. Rejection produces a spec-aligned JSON-RPC
// error envelope back to the user — for requests, an HTTP 200 carrying an
// error response with the originating id; for notifications, an HTTP 400
// carrying an id-less error response per MCP § Streamable HTTP transport.
// Returning a [*RejectError] lets the interceptor pick the JSON-RPC error
// code, message, and data; returning a plain error falls back to a
// generic mapping (see [RejectErrorFromCause]).
//
// Payload mutation is not yet supported — changes to req.JSONRPCMessages
// are silent no-ops and the request body is forwarded verbatim. Header
// mutation on req.UserHTTPRequest.Header is the one exception and does
// flow to the upstream request today. Typed setters for payload
// modification will be introduced when modification becomes a requirement.
type UserRequestInterceptor interface {
	// InterceptUserRequest is called with the parsed inbound request.
	// Implementations may inspect req and should return a non-nil error to
	// reject the request; the interceptor's error is rendered as a
	// JSON-RPC error envelope back to the user and the request is not
	// forwarded to the remote server.
	InterceptUserRequest(ctx context.Context, req *UserRequest) error

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

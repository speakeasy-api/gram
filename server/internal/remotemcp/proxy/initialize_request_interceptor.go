package proxy

import "context"

// InitializeRequestInterceptor runs for each inbound "initialize" JSON-RPC
// request after the generic [UserRequestInterceptor] chain has completed
// and before the request is forwarded to the remote MCP server.
//
// The current contract is inspection and rejection: implementations may
// observe init and return a non-nil error to reject the session-opening
// request. Rejection produces a JSON-RPC error envelope back to the user
// with the originating initialize request id. Returning a [*RejectError]
// lets the interceptor pick the JSON-RPC error code, message, and data;
// returning a plain error falls back to a generic mapping (see
// [RejectErrorFromCause]).
//
// Payload mutation is not yet supported — changes to init.UserRequest or
// init.Params are silent no-ops and the request body is forwarded verbatim.
// Typed setters for payload modification will be introduced when modification
// becomes a requirement.
//
// Non-"initialize" requests are not routed to this interface; implement
// [UserRequestInterceptor] for RPC-agnostic hooks.
type InitializeRequestInterceptor interface {
	// InterceptInitializeRequest is called with the parsed initialize request.
	// Implementations may inspect init and should return a non-nil error to
	// reject the session-opening request; the interceptor's error is surfaced
	// to the user and the request is not forwarded to the remote server.
	InterceptInitializeRequest(ctx context.Context, init *InitializeRequest) error

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

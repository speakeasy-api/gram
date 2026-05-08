package proxy

import "context"

// ToolsCallRequestInterceptor runs for each inbound "tools/call" JSON-RPC
// request after the generic [UserRequestInterceptor] chain has completed
// and before the request is forwarded to the remote MCP server.
//
// The current contract is inspection and rejection: implementations may
// observe call and return a non-nil error to reject the tool invocation.
// Rejection produces a JSON-RPC error envelope back to the user with the
// originating tools/call request id. Returning a [*RejectError] lets the
// interceptor pick the JSON-RPC error code, message, and data; returning
// a plain error falls back to a generic mapping (see
// [RejectErrorFromCause]).
//
// Payload mutation is not yet supported — changes to call.User or call.Params
// are silent no-ops and the request body is forwarded verbatim. Typed setters
// for payload modification will be introduced when modification becomes a
// requirement.
//
// Non-"tools/call" requests are not routed to this interface; implement
// [UserRequestInterceptor] for RPC-agnostic hooks.
type ToolsCallRequestInterceptor interface {
	// InterceptToolsCallRequest is called with the parsed tools/call request.
	// Implementations may inspect call and should return a non-nil error to
	// reject the tool invocation; the interceptor's error is surfaced to the
	// user and the request is not forwarded to the remote server.
	InterceptToolsCallRequest(ctx context.Context, call *ToolsCallRequest) error

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

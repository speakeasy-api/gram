package proxy

import "context"

// ToolsListResponseInterceptor runs for each "tools/list" JSON-RPC response
// returned by the remote MCP server, after the generic
// [RemoteMessageInterceptor] chain has completed and before the response is
// relayed to the user.
//
// The current contract is inspection and rejection: implementations may
// observe list and return a non-nil error to reject the response.
// Rejection produces a JSON-RPC error envelope back to the user with the
// originating tools/list request id — on the JSON path as the response
// body, on the SSE path as a substitute event in place of the rejected
// terminal event. Returning a [*RejectError] lets the interceptor pick
// the JSON-RPC error code, message, and data; returning a plain error
// falls back to a generic mapping (see [RejectErrorFromCause]).
//
// Payload mutation is not yet supported — changes to list.Request,
// list.Result, or list.Error are silent no-ops and the response body is
// relayed verbatim. Typed setters for payload modification will be
// introduced when modification becomes a requirement.
//
// Responses to non-"tools/list" requests are not routed to this interface;
// implement [RemoteMessageInterceptor] for RPC-agnostic hooks.
type ToolsListResponseInterceptor interface {
	// InterceptToolsListResponse is called with the parsed tools/list
	// response. Implementations may inspect list and should return a
	// non-nil error to reject the response; the interceptor's error is
	// surfaced to the user instead of the upstream payload.
	InterceptToolsListResponse(ctx context.Context, list *ToolsListResponse) error

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

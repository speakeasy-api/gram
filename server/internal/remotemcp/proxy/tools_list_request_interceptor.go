package proxy

import "context"

// ToolsListRequestInterceptor runs for each inbound "tools/list" JSON-RPC
// request after the generic [UserRequestInterceptor] chain has completed
// and before the request is forwarded to the remote MCP server.
//
// The current contract is inspection and rejection: implementations may
// observe list and return a non-nil error to reject the tool discovery
// request. Rejection produces a JSON-RPC error envelope back to the user
// with the originating tools/list request id. Returning a [*RejectError]
// lets the interceptor pick the JSON-RPC error code, message, and data;
// returning a plain error falls back to a generic mapping (see
// [RejectErrorFromCause]).
//
// Payload mutation is not yet supported — changes to list.User or list.Params
// are silent no-ops and the request body is forwarded verbatim. Typed setters
// for payload modification will be introduced when modification becomes a
// requirement.
//
// Non-"tools/list" requests are not routed to this interface; implement
// [UserRequestInterceptor] for RPC-agnostic hooks.
type ToolsListRequestInterceptor interface {
	// InterceptToolsListRequest is called with the parsed tools/list request.
	// Implementations may inspect list and should return a non-nil error to
	// reject the discovery request; the interceptor's error is surfaced to
	// the user and the request is not forwarded to the remote server.
	InterceptToolsListRequest(ctx context.Context, list *ToolsListRequest) error

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

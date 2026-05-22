package proxy

import "context"

// ResourcesListRequestInterceptor runs for each inbound "resources/list"
// JSON-RPC request after the generic [UserRequestInterceptor] chain has
// completed and before the request is forwarded to the remote MCP server.
//
// The current contract is inspection and rejection: implementations may
// observe list and return a non-nil error to reject the resource discovery
// request. Rejection produces a JSON-RPC error envelope back to the user
// with the originating resources/list request id. Returning a [*RejectError]
// lets the interceptor pick the JSON-RPC error code, message, and data;
// returning a plain error falls back to a generic mapping (see
// [RejectErrorFromCause]).
//
// Payload mutation has no typed setter on this view today — changes to
// list.UserRequest or list.Params are silent no-ops and the request body
// is forwarded verbatim. Typed setters will be introduced when a concrete
// consumer needs to rewrite resources/list request payloads.
//
// Non-"resources/list" requests are not routed to this interface; implement
// [UserRequestInterceptor] for RPC-agnostic hooks.
type ResourcesListRequestInterceptor interface {
	// InterceptResourcesListRequest is called with the parsed resources/list
	// request. Implementations may inspect list and should return a non-nil
	// error to reject the discovery request; the interceptor's error is
	// surfaced to the user and the request is not forwarded to the remote
	// server.
	InterceptResourcesListRequest(ctx context.Context, list *ResourcesListRequest) error

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

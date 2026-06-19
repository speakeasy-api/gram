package proxy

import "context"

// ResourcesReadRequestInterceptor runs for each inbound "resources/read"
// JSON-RPC request after the generic [UserRequestInterceptor] chain has
// completed and before the request is forwarded to the remote MCP server.
//
// The current contract is inspection and rejection: implementations may
// observe read and return a non-nil error to reject the resource read.
// Rejection produces a JSON-RPC error envelope back to the user with the
// originating resources/read request id. Returning a [*RejectError] lets the
// interceptor pick the JSON-RPC error code, message, and data; returning
// a plain error falls back to a generic mapping (see
// [RejectErrorFromCause]).
//
// Payload mutation has no typed setter on this view today — changes to
// read.UserRequest or read.Params are silent no-ops and the request body is
// forwarded verbatim. Typed setters will be introduced when a concrete
// consumer needs to rewrite resources/read request payloads.
//
// Non-"resources/read" requests are not routed to this interface; implement
// [UserRequestInterceptor] for RPC-agnostic hooks.
type ResourcesReadRequestInterceptor interface {
	// InterceptResourcesReadRequest is called with the parsed resources/read
	// request. Implementations may inspect read and should return a non-nil
	// error to reject the resource read; the interceptor's error is surfaced
	// to the user and the request is not forwarded to the remote server.
	InterceptResourcesReadRequest(ctx context.Context, read *ResourcesReadRequest) error

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

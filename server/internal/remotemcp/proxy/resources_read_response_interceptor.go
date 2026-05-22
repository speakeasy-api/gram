package proxy

import "context"

// ResourcesReadResponseInterceptor runs for each "resources/read" JSON-RPC
// response returned by the remote MCP server, after the generic
// [RemoteMessageInterceptor] chain has completed and before the response is
// relayed to the user.
//
// The current contract is inspection and rejection: implementations may
// observe read and return a non-nil error to reject the response.
// Rejection produces a JSON-RPC error envelope back to the user with the
// originating resources/read request id — on the JSON path as the response
// body, on the SSE path as a substitute event in place of the rejected
// terminal event. Returning a [*RejectError] lets the interceptor pick
// the JSON-RPC error code, message, and data; returning a plain error
// falls back to a generic mapping (see [RejectErrorFromCause]).
//
// Payload mutation has no typed setter on this view today — changes to
// read.Request, read.Result, or read.Error are silent no-ops and the
// response body is relayed verbatim. Typed setters will be introduced
// when a concrete consumer needs to rewrite resources/read response
// payloads.
//
// Responses to non-"resources/read" requests are not routed to this
// interface; implement [RemoteMessageInterceptor] for RPC-agnostic hooks.
type ResourcesReadResponseInterceptor interface {
	// InterceptResourcesReadResponse is called with the parsed resources/read
	// response. Implementations may inspect read and should return a non-nil
	// error to reject the response; the interceptor's error is surfaced to
	// the user instead of the upstream payload.
	InterceptResourcesReadResponse(ctx context.Context, read *ResourcesReadResponse) error

	// Name returns a stable identifier for this interceptor, used for tracing
	// span attributes and log correlation.
	Name() string
}

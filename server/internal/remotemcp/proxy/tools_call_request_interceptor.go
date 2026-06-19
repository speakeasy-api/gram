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
// Payload mutation is available via [ToolsCallRequest.SetArguments],
// which rewrites the call's arguments payload before the request is
// forwarded upstream. Mutations are observed by every subsequent
// interceptor in the same chain through the shared *Params pointer.
// Direct mutation of call.Params fields without going through the setter
// is a silent no-op against the wire — the framework only re-marshals
// the body when a typed setter flips the dirty flag. If a downstream
// interceptor rejects, prior mutations in the chain are discarded; the
// rejection envelope wins and the upstream is never called.
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

package proxy

import "context"

// RemoteMessageInterceptor runs for each JSON-RPC message arriving from the
// remote MCP server, regardless of transport framing. It fires once per
// application/json POST response and once per parseable Server-Sent Event
// in a streamed response.
//
// The contract is inspection and rejection: implementations may observe
// msg and return a non-nil error to block the message from being relayed
// to the user. Payload mutation is not yet supported — changes to
// msg.Message are silent no-ops and the proxy forwards the original bytes.
// Typed setters for payload modification will be introduced when
// modification becomes a requirement.
//
// Rejection produces a spec-aligned JSON-RPC error envelope back to the
// user. The exact wire form depends on the message shape and transport:
//
//   - On an application/json POST response with a request id, the
//     interceptor's rejection becomes the response — HTTP 200 carrying a
//     JSON-RPC error response with the originating id.
//
//   - On a text/event-stream response (POST progress stream or GET
//     stream), the rejected event is replaced inline with a spec-aligned
//     substitute: responses and server-initiated requests become JSON-RPC
//     error responses with the same id; notifications become
//     "notifications/message" log notifications at level "error".
//
// Returning a [*RejectError] lets the interceptor pick the JSON-RPC
// error code, message, and data; returning a plain error falls back to a
// generic mapping (see [RejectErrorFromCause]).
//
// Interceptors run synchronously per message. Slow interceptors delay
// streaming throughput on SSE responses — keep the body cheap.
type RemoteMessageInterceptor interface {
	// InterceptRemoteMessage is called once per JSON-RPC message from the
	// remote. Implementations may inspect msg and should return a non-nil
	// error to block the message from being relayed. The msg pointer is
	// freshly allocated per call and must not be retained past the call's
	// return.
	InterceptRemoteMessage(ctx context.Context, msg *RemoteMessage) error

	// Name returns a stable identifier for this interceptor, used for
	// tracing span attributes and log correlation.
	Name() string
}

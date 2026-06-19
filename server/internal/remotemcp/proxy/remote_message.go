package proxy

import (
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// RemoteMessage captures a single JSON-RPC message arriving from the remote
// MCP server, regardless of transport framing. Instances are constructed by
// the proxy and passed to each [RemoteMessageInterceptor]. For
// application/json POST responses, exactly one RemoteMessage is built per
// request. For text/event-stream responses (POST progress streams or GET
// streams), one RemoteMessage is built per parseable SSE event whose data
// payload decodes as JSON-RPC.
type RemoteMessage struct {
	// UserHTTPRequest is the inbound user request that triggered this
	// message: the POST whose response carried the message, or the GET
	// that opened a server-initiated stream. Available so interceptors
	// can correlate messages back to their initiating request.
	UserHTTPRequest *http.Request

	// RemoteHTTPRequest is the outbound HTTP request the proxy built and
	// sent to the remote MCP server: the URL the proxy resolved to, the
	// method, and the headers the proxy applied (configured static and
	// secret headers, plus any forwarded user headers — minus the Gram
	// Authorization header, which is intentionally stripped). Available
	// so interceptors can inspect exactly what was sent on behalf of the
	// user.
	//
	// Prefer this over [http.Response.Request] on RemoteHTTPResponse:
	//
	//   - Redirects: the proxy uses the stdlib default redirect policy.
	//     If the remote responds with a 3xx, RemoteHTTPResponse.Request
	//     points to the LAST hop in the redirect chain, with potentially
	//     a different URL and a redirect-stripped header set.
	//     RemoteHTTPRequest is always the original outbound request the
	//     proxy built, regardless of redirects.
	//   - Transport mutation: [http.Transport] (and any wrapping
	//     transports such as otelhttp) may add or rewrite headers
	//     (Connection, Accept-Encoding, tracing headers) on the wire
	//     copy. RemoteHTTPRequest reflects the proxy's intent before
	//     transport-level mutation.
	//   - Body: per the stdlib contract, RemoteHTTPResponse.Request.Body
	//     is nil — the body has already been consumed.
	//
	// Interceptors must not read this request's body — it has already
	// been streamed upstream and the underlying reader is exhausted.
	RemoteHTTPRequest *http.Request

	// RemoteHTTPResponse is the upstream HTTP response. Available for
	// header inspection. Interceptors must not read its body — the proxy
	// has already consumed the byte stream and (for SSE) is mid-relay.
	RemoteHTTPResponse *http.Response

	// Message is the decoded JSON-RPC message — typically *jsonrpc.Request
	// for server-initiated requests, *jsonrpc.Response for responses (which
	// includes the terminal tools/call response in an SSE stream), or a
	// notification (a *jsonrpc.Request without an ID).
	Message jsonrpc.Message

	// dirty is true when a typed response-side setter (e.g.
	// [ToolsListResponse.SetTools]) has mutated the underlying JSON-RPC
	// message. [materializedBytes] returns fresh wire bytes that the
	// proxy substitutes for the upstream's original payload before
	// writing to the client.
	//
	// Direct mutation of Message without a setter does not flip this
	// flag and is a silent no-op against the wire — the framework can't
	// know whether a raw mutation was intentional or accidental.
	dirty bool
}

// materializedBytes re-marshals Message to wire bytes and returns them when
// a typed setter has flipped the dirty flag, alongside ok=true. A no-op
// call (no mutation since the message was constructed) returns (nil, false,
// nil). Callers use this to swap the upstream payload for the mutated one
// before writing to the client — JSON path overwrites the response body,
// SSE path re-emits the event via [formatSSEEventWithData].
func (r *RemoteMessage) materializedBytes() ([]byte, bool, error) {
	if !r.dirty {
		return nil, false, nil
	}
	encoded, err := jsonrpc.EncodeMessage(r.Message)
	if err != nil {
		return nil, false, fmt.Errorf("encode mutated jsonrpc message: %w", err)
	}
	r.dirty = false
	return encoded, true, nil
}

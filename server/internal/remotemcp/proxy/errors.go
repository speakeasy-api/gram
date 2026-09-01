package proxy

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ErrBodyTooLarge is returned when a buffered JSON body (either the inbound
// user request or the upstream response) exceeds [Proxy.MaxBufferedBodyBytes]
// during parse. It signals a parse-time allocation guard trip, not a stream
// truncation — streamed responses are not subject to this cap.
var ErrBodyTooLarge = errors.New("body exceeded max size")

// ErrUndecodableJSONRPCBody is returned by [readJSONRPCBody] when the upstream
// returned a non-empty body on the buffered (non-SSE) path that does not decode
// as a single JSON-RPC message — e.g. a bare heartbeat scalar emitted by a
// non-spec-compliant remote. The proxy relays such bodies to the client
// verbatim instead of surfacing a 5xx, mirroring the SSE relay path which skips
// interception for events that fail to decode.
var ErrUndecodableJSONRPCBody = errors.New("upstream response is not a json-rpc message")

// ErrBatchRequest is returned by [UserRequest.ParseJSONRPCMessages] when the
// inbound POST body is a JSON array. MCP Streamable HTTP § Sending Messages to
// the Server disallows batched (array) request bodies in the current spec
// revision, so the proxy rejects them ahead of the JSON-RPC decoder to surface
// a clean envelope (JSON-RPC error code -32600, "batch requests are not
// supported") rather than a generic decode failure.
var ErrBatchRequest = errors.New("batch requests are not supported")

// classifyForwardError maps a [http.Client.Do] failure into a typed proxy
// error. timedOut is true when the failure was caused by the proxy's own
// phase-1 timer firing (vs. a parent-context cancellation from the user
// disconnecting); both surface the same context.Canceled in the http
// error chain so the caller distinguishes via the timer's Stop() return.
func (p *Proxy) classifyForwardError(ctx context.Context, err error, timedOut bool) error {
	switch {
	case timedOut:
		return oops.E(oops.CodeGatewayError, err, "remote mcp server timed out").LogError(ctx, p.Logger)
	case errors.Is(err, context.DeadlineExceeded):
		// Backstop in case any transport-level deadline (e.g.
		// TLSHandshakeTimeout) fires before our phase timer.
		return oops.E(oops.CodeGatewayError, err, "remote mcp server timed out").LogError(ctx, p.Logger)
	case errors.Is(err, context.Canceled):
		return oops.E(oops.CodeBadRequest, err, "client cancelled request").LogError(ctx, p.Logger)
	default:
		return oops.E(oops.CodeGatewayError, err, "remote mcp server unreachable").LogError(ctx, p.Logger)
	}
}

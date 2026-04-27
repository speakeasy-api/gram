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

// classifyForwardError maps a [http.Client.Do] failure into a typed proxy
// error. timedOut is true when the failure was caused by the proxy's own
// phase-1 timer firing (vs. a parent-context cancellation from the user
// disconnecting); both surface the same context.Canceled in the http
// error chain so the caller distinguishes via the timer's Stop() return.
func (p *Proxy) classifyForwardError(ctx context.Context, err error, timedOut bool) error {
	switch {
	case timedOut:
		return oops.E(oops.CodeGatewayError, err, "remote mcp server timed out").Log(ctx, p.Logger)
	case errors.Is(err, context.DeadlineExceeded):
		// Backstop in case any transport-level deadline (e.g.
		// TLSHandshakeTimeout) fires before our phase timer.
		return oops.E(oops.CodeGatewayError, err, "remote mcp server timed out").Log(ctx, p.Logger)
	case errors.Is(err, context.Canceled):
		return oops.E(oops.CodeBadRequest, err, "client cancelled request").Log(ctx, p.Logger)
	default:
		return oops.E(oops.CodeGatewayError, err, "remote mcp server unreachable").Log(ctx, p.Logger)
	}
}

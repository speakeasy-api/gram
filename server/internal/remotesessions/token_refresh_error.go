package remotesessions

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

// errRefreshUpstreamUnreachable marks a refresh POST that never produced an
// answer: DNS, TLS, connection refused, or the POST's own timeout. It is
// wrapped alongside the transport error so the refresh outcome classifier can
// tell an unreachable upstream from a Gram-side failure without inspecting
// error text.
var errRefreshUpstreamUnreachable = errors.New("remotesessions: upstream token endpoint unreachable")

// TokenRefreshError is an operator-actionable failure of a token refresh: a
// condition the caller can understand and act on (revoke and re-link the
// session, fix the issuer's configuration) rather than an internal Gram fault.
//
// Reason is a short, public-safe explanation suitable for surfacing to an
// operator in a UI toast; cause carries the private detail for logs. An
// explicit, user-facing refresh maps these to a client error with the Reason
// shown; the lazy MCP path treats them like any other "no valid token" outcome
// and re-challenges, ignoring the Reason.
type TokenRefreshError struct {
	Reason     string
	cause      error
	code       string
	statusCode int
}

// Error returns the full detail (the public-safe Reason plus the private cause)
// for logs and error chains. It is deliberately NOT the public boundary: code
// surfacing a refresh failure to a client must use the Reason field, never
// Error(), so the cause text never reaches the client.
func (e *TokenRefreshError) Error() string {
	if e.cause == nil {
		return e.Reason
	}
	return e.Reason + ": " + e.cause.Error()
}

func (e *TokenRefreshError) Unwrap() error { return e.cause }

func newTokenRefreshError(reason string, cause error) *TokenRefreshError {
	return &TokenRefreshError{Reason: reason, cause: cause, code: "", statusCode: 0}
}

// invalidGrant reports whether the upstream answered with RFC 6749 §5.2
// invalid_grant, the definitive signal that the stored refresh token can never
// renew, as opposed to a transient failure such as server_error or
// temporarily_unavailable.
func (e *TokenRefreshError) invalidGrant() bool {
	return e.code == oautherr.CodeInvalidGrant
}

// IsTokenRefreshRateLimited reports whether an upstream token endpoint
// explicitly returned HTTP 429. Callers use it to stop contacting that
// provider for the remainder of a best-effort refresh sweep.
func IsTokenRefreshRateLimited(err error) bool {
	var refreshErr *TokenRefreshError
	return errors.As(err, &refreshErr) && refreshErr.statusCode == http.StatusTooManyRequests
}

// newTokenRefreshErrorFromHTTP builds a TokenRefreshError from a non-2xx response
// from the upstream token endpoint. The public Reason is the error body
// normalized onto its RFC 6749 §5.2 members ("invalid_grant: ..."), or "HTTP
// <status>" when the body carried no recognizable error; the raw status and
// body are kept only as the private cause and never surfaced.
func newTokenRefreshErrorFromHTTP(statusCode int, status string, body []byte) *TokenRefreshError {
	reason := "HTTP " + status
	code := ""
	if parsed, ok := oautherr.ParseTokenError(body); ok {
		reason = parsed.Error()
		code = parsed.Code
	}
	return &TokenRefreshError{
		Reason:     reason,
		cause:      fmt.Errorf("refresh endpoint %s: %s", status, string(body)),
		code:       code,
		statusCode: statusCode,
	}
}

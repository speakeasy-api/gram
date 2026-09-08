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
// and re-challenges, ignoring the Reason. When the upstream answered with an
// OAuth error body, upstreamCode is the code exactly as sent and code is its
// canonical RFC 6749 §5.2 form, which is what the grant-clearing decision
// reads.
type TokenRefreshError struct {
	Reason       string
	cause        error
	code         string
	upstreamCode string
	statusCode   int
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

// UpstreamCode returns the error code the upstream token endpoint answered
// with, exactly as sent and before any canonicalization, or "" when the
// failure did not come from a recognizable OAuth error body.
func (e *TokenRefreshError) UpstreamCode() string { return e.upstreamCode }

func newTokenRefreshError(reason string, cause error) *TokenRefreshError {
	return &TokenRefreshError{Reason: reason, cause: cause, code: "", upstreamCode: "", statusCode: 0}
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

// newTokenRefreshErrorFromSuccessBody builds a TokenRefreshError for a 2xx
// token response whose body carries an RFC 6749 §5.2 error instead of a token
// set. GitHub answers a dead refresh token this way (HTTP 200 with
// bad_refresh_token). The status code is kept so the refresh outcome
// classifier files it as an upstream rejection, and an invalid_grant code
// clears the stored refresh grant exactly as a 4xx would. ok is false when the
// body carries no recognizable error.
func newTokenRefreshErrorFromSuccessBody(statusCode int, status string, body []byte) (*TokenRefreshError, bool) {
	parsed, ok := oautherr.ParseTokenError(body)
	if !ok {
		return nil, false
	}
	upstreamCode := parsed.Code
	parsed.Code = oautherr.CanonicalTokenErrorCode(parsed.Code)
	// The body is never embedded: a success-status response may still carry
	// token material next to the error member.
	return &TokenRefreshError{
		Reason:       parsed.Error(),
		cause:        fmt.Errorf("refresh endpoint %s carried error %q", status, upstreamCode),
		code:         parsed.Code,
		upstreamCode: upstreamCode,
		statusCode:   statusCode,
	}, true
}

// newTokenRefreshErrorFromHTTP builds a TokenRefreshError from a non-2xx response
// from the upstream token endpoint. The public Reason is the error body
// normalized onto its RFC 6749 §5.2 members ("invalid_grant: ..."), or "HTTP
// <status>" when the body carried no recognizable error; the raw status and
// body are kept only as the private cause and never surfaced.
func newTokenRefreshErrorFromHTTP(statusCode int, status string, body []byte) *TokenRefreshError {
	reason := "HTTP " + status
	code := ""
	upstreamCode := ""
	if parsed, ok := oautherr.ParseTokenError(body); ok {
		upstreamCode = parsed.Code
		parsed.Code = oautherr.CanonicalTokenErrorCode(parsed.Code)
		reason = parsed.Error()
		code = parsed.Code
	}
	return &TokenRefreshError{
		Reason:       reason,
		cause:        fmt.Errorf("refresh endpoint %s: %s", status, string(body)),
		code:         code,
		upstreamCode: upstreamCode,
		statusCode:   statusCode,
	}
}

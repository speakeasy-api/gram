package remotesessions

import (
	"errors"
	"fmt"
	"net/http"
)

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

func (e *TokenRefreshError) invalidGrant() bool {
	return e.code == oauthErrInvalidGrant
}

// IsTokenRefreshRateLimited reports whether an upstream token endpoint
// explicitly returned HTTP 429. Callers use it to stop contacting that
// provider for the remainder of a best-effort refresh sweep.
func IsTokenRefreshRateLimited(err error) bool {
	var refreshErr *TokenRefreshError
	return errors.As(err, &refreshErr) && refreshErr.statusCode == http.StatusTooManyRequests
}

// newTokenRefreshErrorFromHTTP builds a TokenRefreshError from a non-2xx response
// from the upstream token endpoint. The public Reason summarizes the parsed
// error body (falling back to the HTTP status); the raw status and body are kept
// only as the private cause and never surfaced.
//
// A 4xx body that contains the invalid_grant token is treated as definitive
// even when the provider did not emit a spec-shaped "error" string. 5xx bodies
// are not, so a mention of the code on an upstream error page does not clear
// a still-usable refresh grant.
func newTokenRefreshErrorFromHTTP(statusCode int, status string, body []byte) *TokenRefreshError {
	response := parseTokenErrorResponse(body)
	if response.Error == "" && statusCode >= 400 && statusCode < 500 && containsOAuthErrorToken(string(body), oauthErrInvalidGrant) {
		response.Error = oauthErrInvalidGrant
	}
	return &TokenRefreshError{
		Reason:     response.summary(status),
		cause:      fmt.Errorf("refresh endpoint %s: %s", status, string(body)),
		code:       response.Error,
		statusCode: statusCode,
	}
}

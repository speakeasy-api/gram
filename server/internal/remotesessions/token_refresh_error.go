package remotesessions

import "fmt"

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
	Reason string
	cause  error
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
	return &TokenRefreshError{Reason: reason, cause: cause}
}

// newTokenRefreshErrorFromHTTP builds a TokenRefreshError from a non-2xx response
// from the upstream token endpoint. The public Reason summarizes the RFC 6749
// error body (falling back to the HTTP status); the raw status and body are kept
// only as the private cause and never surfaced.
func newTokenRefreshErrorFromHTTP(status string, body []byte) *TokenRefreshError {
	return newTokenRefreshError(
		parseTokenErrorResponse(body).summary(status),
		fmt.Errorf("refresh endpoint %s: %s", status, string(body)),
	)
}

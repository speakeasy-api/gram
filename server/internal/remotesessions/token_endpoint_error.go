package remotesessions

import "fmt"

// TokenEndpointError is a token endpoint failure reduced to what callers
// classify on. It never carries the provider's response body.
type TokenEndpointError struct {
	// StatusCode is the HTTP status, or 0 when no response arrived.
	StatusCode int

	// Code is the canonical RFC 6749 error code, when the body carried one.
	Code string

	// Transport reports that the request may or may not have reached the
	// provider; it must not be replayed automatically.
	Transport bool

	// Signing reports that the private_key_jwt client assertion could not be
	// signed, so nothing was sent.
	Signing bool
}

func (e *TokenEndpointError) Error() string {
	switch {
	case e.Transport:
		return "token endpoint unreachable"
	case e.Signing:
		return "client assertion signing unavailable"
	case e.Code != "":
		return fmt.Sprintf("token endpoint rejected the grant: status %d, error %s", e.StatusCode, e.Code)
	default:
		return fmt.Sprintf("token endpoint rejected the grant: status %d", e.StatusCode)
	}
}

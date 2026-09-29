package remotesessions

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

// maxTokenEndpointErrorCodeBytes bounds a provider error code carried into
// logs and error messages. Registered codes are under 40 bytes.
const maxTokenEndpointErrorCodeBytes = 64

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

// tokenEndpointErrorCode canonicalizes a provider error code, dropping any code
// that is oversized or outside RFC 6749 §5.2's NQSCHAR set so provider output
// cannot inject control characters or bulk into logs and errors.
func tokenEndpointErrorCode(raw string) string {
	code := oautherr.CanonicalTokenErrorCode(raw)
	if code == "" || len(code) > maxTokenEndpointErrorCodeBytes {
		return ""
	}
	for i := range len(code) {
		if c := code[i]; c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return ""
		}
	}
	return code
}

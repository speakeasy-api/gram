package remotesessions

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

// maxTokenEndpointErrorCodeBytes bounds a provider error code carried into
// logs and error messages. Registered codes are under 40 bytes.
const maxTokenEndpointErrorCodeBytes = 64

// maxProviderErrorDescriptionRunes bounds a provider error_description kept for logs.
const maxProviderErrorDescriptionRunes = 300

// TokenEndpointError is a token endpoint failure reduced to what callers
// classify on. It never carries the provider's response body; Description is
// for logs only and never reaches Error, API responses or audit rows.
type TokenEndpointError struct {
	// StatusCode is the HTTP status, or 0 when no response arrived.
	StatusCode int

	// Code is the canonical RFC 6749 error code, when the body carried one.
	Code string

	// Description is the sanitized RFC 6749 error_description, when present.
	Description string

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
	return providerErrorCode(oautherr.CanonicalTokenErrorCode(raw))
}

// providerErrorCode keeps only a bounded RFC 6749 §5.2 NQSCHAR error code.
func providerErrorCode(code string) string {
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

// ProviderErrorDescription sanitizes a provider error_description for logs only.
func ProviderErrorDescription(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), r == unicode.ReplacementChar, unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, raw)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if runes := []rune(cleaned); len(runes) > maxProviderErrorDescriptionRunes {
		cleaned = string(runes[:maxProviderErrorDescriptionRunes]) + "…"
	}
	return cleaned
}

// providerErrorDescriptionField reads only the top-level error_description member.
func providerErrorDescriptionField(body []byte) string {
	var e struct {
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &e)
	return ProviderErrorDescription(e.Description)
}

package remotesessions

import (
	"encoding/json"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// oauthErrInvalidGrant is the RFC 6749 §5.2 error code an OAuth 2.0 token
// endpoint returns when a grant (here, a refresh_token) is invalid, expired, or
// revoked. It is the definitive signal that the stored refresh token can never
// renew, distinct from transient failures (server_error, temporarily_unavailable).
const oauthErrInvalidGrant = "invalid_grant"

// tokenErrorResponse is the public-safe view of an upstream token-endpoint
// error: an OAuth error code plus optional description. RFC 6749 §5.2 members
// are preferred; well-known provider shapes (an errors[] of invalid_grant
// messages, a nested error object) are normalized onto the same fields.
type tokenErrorResponse struct {
	Error            string
	ErrorDescription string
	ErrorURI         string
}

type rawTokenErrorResponse struct {
	Error            json.RawMessage `json:"error"`
	ErrorDescription string          `json:"error_description"`
	ErrorURI         string          `json:"error_uri"`
	Errors           []string        `json:"errors"`
}

type nestedTokenError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func newTokenError(code, description, uri string) tokenErrorResponse {
	return tokenErrorResponse{
		Error:            code,
		ErrorDescription: description,
		ErrorURI:         uri,
	}
}

// parseTokenErrorResponse decodes an upstream token error body. A body that
// does not decode, or that carries no recognizable error, yields the zero
// value — whose summary falls back to the HTTP status.
func parseTokenErrorResponse(body []byte) tokenErrorResponse {
	var raw rawTokenErrorResponse
	if json.Unmarshal(body, &raw) != nil {
		return newTokenError("", "", "")
	}

	if e, ok := parseErrorMember(raw.Error); ok {
		if e.ErrorDescription == "" {
			e.ErrorDescription = raw.ErrorDescription
		}
		if e.ErrorURI == "" {
			e.ErrorURI = raw.ErrorURI
		}
		return e
	}

	for _, msg := range raw.Errors {
		if e, ok := splitInvalidGrant(msg); ok {
			return e
		}
	}

	return newTokenError("", raw.ErrorDescription, raw.ErrorURI)
}

// summary renders a short, public-safe description of the error, preferring the
// OAuth error / error_description and falling back to the supplied HTTP status
// when the body carried no recognizable error. The raw body is never surfaced.
func (e tokenErrorResponse) summary(status string) string {
	switch {
	case e.Error != "" && e.ErrorDescription != "":
		return e.Error + ": " + e.ErrorDescription
	case e.Error != "" && e.ErrorURI != "":
		return e.Error + ": " + e.ErrorURI
	case e.Error != "":
		return e.Error
	default:
		return "HTTP " + status
	}
}

func parseErrorMember(raw json.RawMessage) (tokenErrorResponse, bool) {
	if len(raw) == 0 {
		return newTokenError("", "", ""), false
	}

	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return newTokenError("", "", ""), false
		}
		if e, ok := splitInvalidGrant(s); ok {
			return e, true
		}
		return newTokenError(s, "", ""), true
	}

	var nested nestedTokenError
	if json.Unmarshal(raw, &nested) != nil {
		return newTokenError("", "", ""), false
	}

	code := strings.TrimSpace(conv.Default(nested.Code, nested.Error))
	desc := strings.TrimSpace(nested.Message)
	if code == "" && desc == "" {
		return newTokenError("", "", ""), false
	}

	if e, ok := splitInvalidGrant(code); ok {
		if e.ErrorDescription == "" {
			e.ErrorDescription = desc
		}
		return e, true
	}
	if isDeadRefreshGrant(code, desc) {
		return newTokenError(oauthErrInvalidGrant, conv.Default(desc, code), ""), true
	}
	if code == "" {
		return newTokenError("", "", ""), false
	}
	return newTokenError(code, desc, ""), true
}

// splitInvalidGrant reports whether msg is an invalid_grant signal: it starts
// with that code (optionally followed by a separator and description), or it
// contains invalid_grant as a standalone token.
func splitInvalidGrant(msg string) (tokenErrorResponse, bool) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return newTokenError("", "", ""), false
	}

	if rest, ok := strings.CutPrefix(msg, oauthErrInvalidGrant); ok {
		if rest == "" || !isErrorTokenByte(rest[0]) {
			rest = strings.TrimSpace(strings.TrimLeft(rest, "-:."))
			return newTokenError(oauthErrInvalidGrant, rest, ""), true
		}
	}
	if containsOAuthErrorToken(msg, oauthErrInvalidGrant) {
		return newTokenError(oauthErrInvalidGrant, msg, ""), true
	}
	return newTokenError("", "", ""), false
}

// containsOAuthErrorToken reports whether token appears in s with non-identifier
// boundaries, so "invalid_grant_extra" does not match "invalid_grant".
func containsOAuthErrorToken(s, token string) bool {
	start := 0
	for {
		i := strings.Index(s[start:], token)
		if i < 0 {
			return false
		}
		i += start
		beforeOK := i == 0 || !isErrorTokenByte(s[i-1])
		after := i + len(token)
		afterOK := after == len(s) || !isErrorTokenByte(s[after])
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
	}
}

func isErrorTokenByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// isDeadRefreshGrant reports whether a nested provider error is a refresh
// grant that can never succeed again. Dub encodes this as unauthorized plus a
// "Refresh token not found." message rather than RFC 6749 invalid_grant.
func isDeadRefreshGrant(code, message string) bool {
	if !strings.EqualFold(code, "unauthorized") {
		return false
	}
	m := strings.ToLower(message)
	if !strings.Contains(m, "refresh token") {
		return false
	}
	for _, needle := range []string{"not found", "invalid", "expired", "revoked", "unknown"} {
		if strings.Contains(m, needle) {
			return true
		}
	}
	return false
}

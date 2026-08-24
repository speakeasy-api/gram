package remotesessions

import (
	"encoding/json"
	"net/http"
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
// are preferred; the known provider shapes recognized by the dialect matchers
// below are normalized onto the same fields.
type tokenErrorResponse struct {
	Error            string
	ErrorDescription string
	ErrorURI         string
}

func newTokenError(code, description, uri string) tokenErrorResponse {
	return tokenErrorResponse{
		Error:            code,
		ErrorDescription: description,
		ErrorURI:         uri,
	}
}

// nonRFCErrorDefinitive reports whether statusCode can carry a definitive
// invalid_grant signal from a non-RFC provider shape. 5xx pages, rate limits
// (429), and request timeouts (408) are never definitive: a false positive
// here permanently clears a still-usable refresh grant.
func nonRFCErrorDefinitive(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 &&
		statusCode != http.StatusRequestTimeout &&
		statusCode != http.StatusTooManyRequests
}

// parseTokenErrorResponse decodes an upstream token error body. Members are
// decoded independently, so a provider extension with an unexpected shape
// cannot discard an RFC 6749 error code sitting next to it. Beyond the RFC
// members, only explicitly recognized provider dialects (Datadog, Dub) can
// classify — free text never does. A body that does not decode, or that
// carries no recognizable error, yields the zero value — whose summary falls
// back to the HTTP status.
func parseTokenErrorResponse(statusCode int, body []byte) tokenErrorResponse {
	var members map[string]json.RawMessage
	if json.Unmarshal(body, &members) != nil {
		return newTokenError("", "", "")
	}

	var desc, uri string
	_ = json.Unmarshal(members["error_description"], &desc)
	_ = json.Unmarshal(members["error_uri"], &uri)

	var code string
	if json.Unmarshal(members["error"], &code) == nil && strings.TrimSpace(code) != "" {
		return newTokenError(strings.TrimSpace(code), desc, uri)
	}

	if nonRFCErrorDefinitive(statusCode) {
		e, ok := datadogInvalidGrant(members["errors"])
		if !ok {
			e, ok = nestedProviderError(members["error"])
		}
		if ok {
			e.ErrorDescription = conv.Default(e.ErrorDescription, desc)
			e.ErrorURI = conv.Default(e.ErrorURI, uri)
			return e
		}
	}

	return newTokenError("", desc, uri)
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

// datadogInvalidGrant matches Datadog's token error dialect: an errors[] array
// with an entry of the form "invalid_grant - <description>". Only entries that
// begin with the code count; a mention elsewhere in a message never does.
func datadogInvalidGrant(raw json.RawMessage) (tokenErrorResponse, bool) {
	var entries []json.RawMessage
	_ = json.Unmarshal(raw, &entries)
	for _, entry := range entries {
		var msg string
		if json.Unmarshal(entry, &msg) != nil {
			continue
		}
		rest, ok := strings.CutPrefix(strings.TrimSpace(msg), oauthErrInvalidGrant)
		if !ok || (rest != "" && isErrorTokenByte(rest[0])) {
			continue
		}
		rest = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), "-:."))
		return newTokenError(oauthErrInvalidGrant, rest, ""), true
	}
	return newTokenError("", "", ""), false
}

// isErrorTokenByte reports whether b can be part of an OAuth error code, so
// "invalid_grant_extra" does not match "invalid_grant".
func isErrorTokenByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// deadGrantMessages are literal (lowercase, period-stripped) provider messages
// that report the refresh grant itself as permanently unusable under a generic
// "unauthorized" code. Dub is the only known producer. Add new literals as
// providers are observed in logs — never patterns: fuzzy matching here has
// repeatedly misclassified recoverable client-auth failures, and a false
// positive irreversibly clears live grants.
var deadGrantMessages = []string{
	"refresh token not found",
}

// nestedProviderError matches the nested-object dialect used by Dub and
// similar APIs: {"error": {"code": ..., "message": ...}}. An explicit
// invalid_grant code counts, as does a known dead-grant literal under
// "unauthorized"; any other code passes through unclassified.
func nestedProviderError(raw json.RawMessage) (tokenErrorResponse, bool) {
	var nested struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &nested) != nil {
		return newTokenError("", "", ""), false
	}

	code := strings.TrimSpace(conv.Default(nested.Code, nested.Error))
	msg := strings.TrimSpace(nested.Message)
	if code == oauthErrInvalidGrant {
		return newTokenError(oauthErrInvalidGrant, msg, ""), true
	}
	if strings.EqualFold(code, "unauthorized") {
		normalized := strings.TrimRight(strings.ToLower(msg), ".")
		for _, dead := range deadGrantMessages {
			if normalized == dead {
				return newTokenError(oauthErrInvalidGrant, msg, ""), true
			}
		}
	}
	if code == "" {
		return newTokenError("", "", ""), false
	}
	return newTokenError(code, msg, ""), true
}

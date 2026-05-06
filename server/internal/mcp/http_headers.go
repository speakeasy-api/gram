package mcp

import (
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// AuthorizationBearerToken returns the Bearer token from the request's
// Authorization header with the "Bearer " prefix stripped. Prefix matching
// is case-insensitive per RFC 7235 § 2.1 ("Bearer" / "BEARER" / "bearer").
// Returns "" when the header is absent or carries a non-Bearer scheme — a
// non-empty return is always a Bearer token, never the raw header. This
// matters for callers that synthesise an upstream Authorization header
// from the result; forwarding a non-Bearer value would produce a malformed
// "Bearer <other-scheme> ..." upstream.
func AuthorizationBearerToken(r *http.Request) string {
	const bearerPrefix = "Bearer "
	token := r.Header.Get("Authorization")
	if len(token) >= len(bearerPrefix) && strings.EqualFold(token[:len(bearerPrefix)], bearerPrefix) {
		return token[len(bearerPrefix):]
	}
	return ""
}

// AuthorizationOrChatSessionToken returns the caller's identity token,
// drawn from the Authorization Bearer header when present, otherwise the
// Gram-Chat-Session header. Returns "" when neither carries a value.
// Bearer wins when both are set, matching the priority used by the MCP
// identity-auth path; non-Bearer Authorization schemes are ignored (see
// [AuthorizationBearerToken]) and fall through to Gram-Chat-Session.
func AuthorizationOrChatSessionToken(r *http.Request) string {
	if t := AuthorizationBearerToken(r); t != "" {
		return t
	}
	return r.Header.Get(constants.ChatSessionsTokenHeader)
}

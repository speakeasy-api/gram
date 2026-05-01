package mcp

import (
	"net/http"
	"strings"
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

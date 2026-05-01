package mcp

import (
	"net/http"
	"strings"
)

// AuthorizationBearerToken returns the Bearer token from the request's
// Authorization header with the "Bearer " prefix stripped. Prefix matching
// is case-insensitive per RFC 7235 § 2.1 ("Bearer" / "BEARER" / "bearer").
// Returns "" when the header is absent.
func AuthorizationBearerToken(r *http.Request) string {
	const bearerPrefix = "Bearer "
	token := r.Header.Get("Authorization")
	if len(token) >= len(bearerPrefix) && strings.EqualFold(token[:len(bearerPrefix)], bearerPrefix) {
		return token[len(bearerPrefix):]
	}
	return token
}

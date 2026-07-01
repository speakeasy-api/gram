// Package httpheaders contains shared MCP request header parsing helpers.
package httpheaders

import (
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// AuthorizationBearerToken returns the Bearer token from the request's
// Authorization header with the "Bearer " prefix stripped.
func AuthorizationBearerToken(r *http.Request) string {
	const bearerPrefix = "Bearer "
	token := r.Header.Get("Authorization")
	if len(token) >= len(bearerPrefix) && strings.EqualFold(token[:len(bearerPrefix)], bearerPrefix) {
		return token[len(bearerPrefix):]
	}
	return ""
}

// AuthorizationOrChatSessionToken returns the caller's identity token from the
// Authorization header when present, otherwise from Gram-Chat-Session.
func AuthorizationOrChatSessionToken(r *http.Request) string {
	const bearerPrefix = "Bearer "
	raw := r.Header.Get("Authorization")
	if len(raw) >= len(bearerPrefix) && strings.EqualFold(raw[:len(bearerPrefix)], bearerPrefix) {
		raw = raw[len(bearerPrefix):]
	}
	if raw != "" {
		return raw
	}
	return r.Header.Get(constants.ChatSessionsTokenHeader)
}

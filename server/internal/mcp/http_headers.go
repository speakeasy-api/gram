package mcp

import (
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// AuthorizationBearerToken returns the token portion of the Authorization
// header. When the header carries a "Bearer " prefix (case-insensitive per
// RFC 7235 § 2.1), the prefix is stripped; otherwise the raw header value
// is returned verbatim.
//
// DO NOT make this strict (i.e. return "" for non-Bearer headers). The
// hosted MCP install page (server/internal/mcpmetadata/hosted_page.html.tmpl)
// emits "Authorization:${GRAM_KEY}" with no scheme, so users paste their raw
// Gram API key into the env var and the wire header lands as
// "Authorization: <key>" with no Bearer prefix. PR #2540 briefly switched
// this to strict matching and broke every existing private MCP install —
// don't repeat that. If you ever do tighten this, update the install-page
// snippets to emit a Bearer prefix in the same change.
func AuthorizationBearerToken(r *http.Request) string {
	const bearerPrefix = "Bearer "
	token := r.Header.Get("Authorization")
	if len(token) >= len(bearerPrefix) && strings.EqualFold(token[:len(bearerPrefix)], bearerPrefix) {
		return token[len(bearerPrefix):]
	}
	return token
}

// AuthorizationOrChatSessionToken returns the caller's identity token,
// drawn from the Authorization header when present, otherwise the
// Gram-Chat-Session header. Returns "" when neither carries a value.
// Authorization wins when both are set, matching the priority used by the
// MCP identity-auth path. See [AuthorizationBearerToken] for the lenient
// handling of headers that lack a "Bearer " prefix.
func AuthorizationOrChatSessionToken(r *http.Request) string {
	if t := AuthorizationBearerToken(r); t != "" {
		return t
	}
	return r.Header.Get(constants.ChatSessionsTokenHeader)
}

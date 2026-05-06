// OAuth authorization code exchange handlers for MCP clients. Issuer-gated
// toolsets (toolsets.user_session_issuer_id set) flow through the OAuth 2.1
// + RFC 7591 / RFC 9728 handlers in this file; toolsets without an issuer
// fall through to the legacy paths in wellknown.Resolve*.
//
// Handlers in this file:
//
//   - WriteAuthenticateChallenge — 401 + WWW-Authenticate; points clients
//     at RFC 9728 discovery.
//   - HandleGetProtectedResource — GET /.well-known/oauth-protected-resource/mcp/{slug}.
//   - HandleGetAuthorizationServer — GET /.well-known/oauth-authorization-server/mcp/{slug}.
//   - HandleRegister — POST /mcp/{slug}/register (RFC 7591 DCR).

package mcp

import (
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// WriteAuthenticateChallenge sets the WWW-Authenticate header and returns an
// oops.CodeUnauthorized error. The 401 status and response body still come
// from the oops error middleware; the helper owns only the header — that
// behaviour is identical to today's inline writes.
//
// Header shape (RFC 9728 §5.3):
//
//	Bearer resource_metadata="<base>/.well-known/oauth-protected-resource/mcp/<slug>"
//
// This is the seam where future user_session_issuer-driven shape extensions
// (e.g. richer `error=` / `realm=` parameters) will land.
func WriteAuthenticateChallenge(w http.ResponseWriter, baseURL, mcpSlug, message string) error {
	w.Header().Set(
		"WWW-Authenticate",
		fmt.Sprintf(`Bearer resource_metadata="%s"`, baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug),
	)
	if message == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	return oops.E(oops.CodeUnauthorized, nil, "%s", message)
}

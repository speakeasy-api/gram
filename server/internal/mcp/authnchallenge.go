// Package mcp — authnchallenge.go
//
// Linear, top-to-bottom view of the Gram-as-Authorization-Server authn
// challenge flow for MCP clients. Read this file end-to-end to follow what
// happens between an unauthenticated `/mcp/{slug}` request and a signed
// SessionClaims JWT (per spike §4.5) coming back to the client.
//
// The flow follows the OAuth 2.1 + RFC 7591 / RFC 9728 dance, gated on
// toolsets.user_session_issuer_id. Legacy paths stay unchanged when the
// column is unset.
//
// Order of operations (each handler lands in its own commit):
//
//   1. WriteAuthenticateChallenge — 401 with WWW-Authenticate; kicks the
//      unauthenticated client into RFC 9728 discovery.
//   2. HandleGetProtectedResource — GET <new path TBD>.
//   3. HandleGetAuthorizationServer — GET <new path TBD>.
//   4. HandleRegister — POST /mcp/{slug}/register (RFC 7591 DCR).
//   5. HandleAuthorize — GET /mcp/{slug}/authorize.
//   6. HandleClientLoginCallback — GET /mcp/{slug}/client_login_callback
//      (Speakeasy IDP returns here on the private-toolset path).
//   7. HandleConsent — GET, POST /mcp/{slug}/connect.
//   8. HandleToken — POST /mcp/{slug}/token (auth-code grant).
//   9. HandleRevoke — POST /mcp/{slug}/revoke (RFC 7009).
//
// Routes 4–9 are advertised by the AS metadata document built in step 3 so
// MCP clients see a coherent set even before each handler is implemented.
//
// Wiring policy: the existing legacy code paths (mcp/impl.go inline
// WWW-Authenticate writes, the well-known handlers at the canonical RFC
// paths) are left untouched. The handlers in this file serve issuer-gated
// traffic via separate routes; clients are pointed at them through the
// resource_metadata parameter on the new path's WWW-Authenticate. The
// WriteAuthenticateChallenge helper below is therefore unused at the time
// of its introduction — call sites land in commits 4 onward.

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

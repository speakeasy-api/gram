package cimd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// validateClientIDURL enforces the -02 §3 Client Identifier URL syntax rules
// on the presented client_id and returns the parsed URL:
//
//   - https scheme (IsClientIDURL gates entry, so a violation here means a
//     mixed-case or otherwise mangled prefix)
//   - no userinfo component
//   - a path component is required (bare https://example.com is invalid)
//   - no "." or ".." path segments
//   - no fragment
//   - a query string is tolerated (clients SHOULD NOT use one)
//
// No normalization is applied anywhere — -02 §3 mandates simple string
// comparison per RFC 3986 §6.2.1, so https://example.com/c and
// https://example.com:443/c are different client identifiers.
func validateClientIDURL(clientID string) (*url.URL, error) {
	if len(clientID) > maxClientIDLength {
		return nil, &usersessions.OAuthError{Code: "invalid_request", Description: fmt.Sprintf("client_id exceeds the %d byte limit", maxClientIDLength)}
	}
	if !strings.HasPrefix(clientID, "https://") {
		return nil, &usersessions.OAuthError{Code: "invalid_request", Description: "client_id URL must use the https scheme"}
	}
	// url.Parse tolerates fragments by splitting them off; detect them on
	// the raw string so an empty "#" is caught too.
	if strings.Contains(clientID, "#") {
		return nil, &usersessions.OAuthError{Code: "invalid_request", Description: "client_id URL must not contain a fragment"}
	}

	parsed, err := url.Parse(clientID)
	if err != nil {
		return nil, &usersessions.OAuthError{Code: "invalid_request", Description: "client_id is not a valid URL"}
	}
	if parsed.User != nil {
		return nil, &usersessions.OAuthError{Code: "invalid_request", Description: "client_id URL must not contain a userinfo component"}
	}
	if parsed.Host == "" {
		return nil, &usersessions.OAuthError{Code: "invalid_request", Description: "client_id URL must include a host"}
	}
	if parsed.Path == "" {
		return nil, &usersessions.OAuthError{Code: "invalid_request", Description: "client_id URL must include a path component"}
	}
	for segment := range strings.SplitSeq(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, &usersessions.OAuthError{Code: "invalid_request", Description: `client_id URL must not contain "." or ".." path segments`}
		}
	}

	return parsed, nil
}

// validateDocument applies the -02 §4 document rules plus Gram policy to a
// parsed document. clientID is both the client_id the OAuth client presented
// and the URL that was fetched (the fetcher never follows redirects), so the
// single equality check here completes the §4 triple equality requirement.
func validateDocument(doc *Document, clientID string, clientIDURL *url.URL) error {
	if doc.ClientID != clientID {
		return &usersessions.OAuthError{Code: "invalid_client_metadata", Description: "document client_id does not match the client identifier URL"}
	}
	// MCP requires client_name, and the user_session_clients.client_name
	// column is NOT NULL — requiring it here avoids fallback logic.
	if doc.ClientName == "" {
		return &usersessions.OAuthError{Code: "invalid_client_metadata", Description: "client_name is required"}
	}
	if len(doc.ClientName) > maxClientNameLength {
		return &usersessions.OAuthError{Code: "invalid_client_metadata", Description: fmt.Sprintf("client_name exceeds the %d byte limit", maxClientNameLength)}
	}
	// Only public clients are accepted. -02 §8.2's direction of travel is that
	// capable clients SHOULD authenticate (MCP clients MAY use
	// private_key_jwt — ChatGPT's documents do); accepting those is
	// deferred, and when it lands §8.2's RFC 7523 §2.2 enforcement MUST
	// come with it. An absent value is rejected too: the RFC 7591 default
	// is client_secret_basic, a symmetric method the spec bans for CIMD.
	if doc.TokenEndpointAuthMethod != "none" {
		return &usersessions.OAuthError{Code: "invalid_client_metadata", Description: `token_endpoint_auth_method must be "none"`}
	}
	// -02 §4.1: a metadata document is public, so it must never carry a
	// client secret in any form. Presence alone invalidates the document.
	if doc.ClientSecret != nil || doc.ClientSecretExpiresAt != nil {
		return &usersessions.OAuthError{Code: "invalid_client_metadata", Description: "document must not contain client_secret or client_secret_expires_at"}
	}
	if err := validateJWKSPublicOnly(doc.JWKS); err != nil {
		return err
	}

	if len(doc.RedirectURIs) == 0 {
		return &usersessions.OAuthError{Code: "invalid_redirect_uri", Description: "redirect_uris is required"}
	}
	if len(doc.RedirectURIs) > maxRedirectURIs {
		return &usersessions.OAuthError{Code: "invalid_redirect_uri", Description: fmt.Sprintf("redirect_uris exceeds the limit of %d entries", maxRedirectURIs)}
	}
	for _, uri := range doc.RedirectURIs {
		if len(uri) > maxRedirectURILength {
			return &usersessions.OAuthError{Code: "invalid_redirect_uri", Description: fmt.Sprintf("redirect_uri exceeds the %d byte limit", maxRedirectURILength)}
		}
		if err := usersessions.ValidateRedirectURI(uri); err != nil {
			return fmt.Errorf("validate redirect_uri: %w", err)
		}
		if err := validateOriginBinding(clientIDURL, uri); err != nil {
			return err
		}
	}

	return nil
}

// validateOriginBinding enforces Gram's CIMD redirect-URI origin-binding
// policy: every redirect_uri in the document must share the client_id URL's
// origin (scheme + host + port, compared without normalization), except
// loopback redirects, which are always allowed on any port. This is
// deliberate Gram policy expressly permitted by -02 §8.1, not a spec
// requirement — under open admission the client_id origin is the only
// identity anchor, and binding ensures authorization codes are only ever
// delivered to that origin (or to loopback). All known vendor CIMD documents
// pass this rule (verified 2026-07); a legitimate cross-origin client would
// need a per-URL exemption on the admission allowlist (follow-up work).
func validateOriginBinding(clientIDURL *url.URL, redirectURI string) error {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return &usersessions.OAuthError{Code: "invalid_redirect_uri", Description: "redirect_uri must be an absolute URL"}
	}
	if IsLoopbackRedirectURI(parsed) {
		return nil
	}
	if parsed.Scheme != clientIDURL.Scheme || parsed.Host != clientIDURL.Host {
		return &usersessions.OAuthError{Code: "invalid_redirect_uri", Description: fmt.Sprintf("redirect_uri %q is not same-origin with the client_id URL", redirectURI)}
	}
	return nil
}

// IsLoopbackRedirectURI reports whether a parsed redirect URI is an RFC 8252
// §7.3 loopback redirect: http scheme with a host of 127.0.0.1, ::1, or
// localhost, on any port. Shared with the mcp package's request-time
// redirect matching, where RFC 8252 requires variable loopback ports to be
// accepted for native apps.
func IsLoopbackRedirectURI(u *url.URL) bool {
	if u.Scheme != "http" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

// validateJWKSPublicOnly rejects a jwks member containing private or
// symmetric key material (-02 §4.1 — new in -02). A public metadata document
// carrying a private key would let anyone impersonate the client, so the
// whole document is invalid. Detection keys on the JWK members that only
// appear on non-public keys: "d" (RSA / EC / OKP private component) and "k"
// (symmetric oct key).
func validateJWKSPublicOnly(raw json.RawMessage) error {
	if raw == nil {
		return nil
	}

	var jwks struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &jwks); err != nil {
		return &usersessions.OAuthError{Code: "invalid_client_metadata", Description: "jwks is not a valid JWK Set"}
	}
	for _, key := range jwks.Keys {
		if _, ok := key["d"]; ok {
			return &usersessions.OAuthError{Code: "invalid_client_metadata", Description: "jwks must not contain private key material"}
		}
		if _, ok := key["k"]; ok {
			return &usersessions.OAuthError{Code: "invalid_client_metadata", Description: "jwks must not contain symmetric key material"}
		}
	}
	return nil
}

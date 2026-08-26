// Package oauthwire holds the pieces of the issuer-gated OAuth surface that
// both the usersessions management API and its cimd subpackages need: the
// wire-error shape and the redirect-URI scheme rules.
//
// It exists to be a leaf. The cimd resolver has to construct OAuth errors
// and validate redirect URIs, and the management API has to read the CIMD
// preset catalog — without a shared leaf those two requirements form an
// import cycle between usersessions and cimd. Keeping only
// stdlib-dependent primitives here makes the cycle impossible to
// reintroduce.
package oauthwire

import (
	"fmt"
	"net/url"
	"strings"
)

// Client authentication methods as they appear on the wire: the
// token_endpoint_auth_method values of RFC 7591 §2 and the RFC 8414
// token_endpoint_auth_methods_supported list. Every authorization server in
// this codebase builds its accepted set from these rather than from literals,
// so the string that is validated at registration, persisted on the client
// row, and matched at the token endpoint is one identifier in three places.
const (
	// AuthMethodClientSecretBasic presents the client secret as HTTP Basic
	// credentials (RFC 6749 §2.3.1).
	AuthMethodClientSecretBasic = "client_secret_basic"

	// AuthMethodClientSecretPost presents the client secret in the request
	// body (RFC 6749 §2.3.1).
	AuthMethodClientSecretPost = "client_secret_post"

	// AuthMethodNone is a public client: no credential, with PKCE or
	// refresh-token possession as the integrity proof.
	AuthMethodNone = "none"

	// AuthMethodPrivateKeyJWT authenticates with an RFC 7523 §2.2 assertion
	// signed by a key the client publishes (RFC 7591 §2, OIDC Core §9). The
	// only method here that proves possession of something never sent to
	// the server.
	AuthMethodPrivateKeyJWT = "private_key_jwt"
)

// Error carries an OAuth wire error: the shared shape used across the
// issuer-gated endpoints (RFC 6749 / RFC 7591 / RFC 7009). The structure is
// identical everywhere — error code plus human-readable description — so
// request validation can return a uniform error and let each handler decide
// how to write it (redirect for /authorize, JSON for /token + /register,
// status 200 for /revoke).
type Error struct {
	Code        string
	Description string
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

// ValidateRedirectURI enforces the OAuth 2.1 / RFC 8252 redirect-URI scheme
// rules:
//
//   - https://... for web + confidential clients.
//   - http://... only when the host is a loopback literal (127.0.0.1, ::1,
//     localhost) per RFC 8252 §7.3.
//   - custom-scheme://... for native apps. RFC 8252 §7.1 recommends
//     reverse-DNS form (com.example.app) to make collisions between
//     independent apps unlikely, but that is NOT enforced here: any scheme
//     outside the blocklist below is accepted, dotted or not. See AIS-434.
//
// Dangerous schemes (javascript:, data:, vbscript:, file:, blob:, etc.)
// are rejected unconditionally — they would let a registered redirect_uri
// turn the AS's 302 Location into an XSS or local-file fetch vector.
func ValidateRedirectURI(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return &Error{Code: "invalid_redirect_uri", Description: "redirect_uri must be an absolute URL"}
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "https":
		if parsed.Host == "" {
			return &Error{Code: "invalid_redirect_uri", Description: "redirect_uri must include a host"}
		}
		return nil
	case "http":
		// RFC 8252 §7.3 loopback hosts only. parsed.Hostname() strips any
		// :port suffix.
		host := strings.ToLower(parsed.Hostname())
		switch host {
		case "127.0.0.1", "::1", "localhost":
			return nil
		default:
			return &Error{Code: "invalid_redirect_uri", Description: "http redirect_uri is only allowed for loopback hosts (127.0.0.1, ::1, localhost)"}
		}
	default:
		// Native-app custom scheme. Only the well-known dangerous schemes
		// are rejected. The RFC 8252 §7.1 reverse-DNS recommendation is
		// deliberately not enforced (AIS-434): adding it would change which
		// already-registered clients validate, so it is tracked separately
		// rather than folded into an unrelated change.
		switch scheme {
		case "javascript", "data", "vbscript", "file", "blob", "view-source":
			return &Error{Code: "invalid_redirect_uri", Description: fmt.Sprintf("redirect_uri scheme %q is not permitted", scheme)}
		}
		return nil
	}
}

// ResourceIndicatorFrom extracts the RFC 8707 `resource` parameter from a query
// string or form body, returning nil when the parameter is absent and a pointer
// to the raw value when it is present. The distinction is load-bearing: RFC 8707
// §2 lets a client omit the parameter, but requires any value it does send to be
// an absolute URI, so `resource=` is a malformed value rather than an omission
// and must not be waved through as one.
func ResourceIndicatorFrom(values url.Values) *string {
	if !values.Has("resource") {
		return nil
	}
	raw := values.Get("resource")
	return &raw
}

// ValidateResourceIndicator checks an RFC 8707 `resource` parameter against
// canonical, the resource identifier for the address the request arrived on. A
// nil resource is accepted: RFC 8707 §2 leaves demanding the parameter to the
// authorization server's discretion, and clients predating MCP 2026-07-28 do not
// send one. A present-but-empty value is rejected like any other non-matching
// one, since the empty string is not the absolute URI the RFC requires.
//
// Comparison is byte equality. MCP 2026-07-28 asks implementations to accept
// uppercase scheme and host components for robustness; this surface
// deliberately declines, holding `resource` to the simple string comparison
// (RFC 3986 §6.2.1) that RFC 9207 §2.4 mandates for `iss`. The two identifiers
// are minted from one base URL and published in the same metadata documents,
// so a client echoing the value it read back matches on the first attempt.
//
// canonical is address-specific — one MCP server is reachable under several
// identifiers (custom domain or platform origin, each under two route bases).
// Callers must derive it from the request being validated, never from a stored
// or global URL.
func ValidateResourceIndicator(resource *string, canonical string) error {
	if resource == nil {
		return nil
	}
	if *resource != canonical {
		// The submitted value is deliberately not echoed: on the authorize leg
		// this description is carried in a redirect the client renders. The
		// expected value is already public in the protected-resource metadata.
		return &Error{Code: "invalid_target", Description: fmt.Sprintf("resource does not identify this MCP server (expected %q)", canonical)}
	}
	return nil
}

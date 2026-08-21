// Package oauthwire holds the pieces of the issuer-gated OAuth surface that
// both the usersessions management API and its cimd subpackages need: the
// wire-error shape, the redirect-URI scheme rules, and the request-time
// redirect-URI matcher.
//
// It exists to be a leaf. The cimd resolver has to construct OAuth errors
// and validate redirect URIs, and the management API has to read the CIMD
// preset catalog — without a shared leaf those two requirements form an
// import cycle between usersessions and cimd. Keeping only
// stdlib-dependent primitives here makes the cycle impossible to
// reintroduce. The Platform MCP authorization server, which shares neither
// package's persistence, reaches the same rules through this leaf.
package oauthwire

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
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

// IsLoopbackRedirectURI reports whether a parsed redirect URI is an RFC 8252
// §7.3 loopback redirect: http scheme with a host of 127.0.0.1, ::1, or
// localhost, on any port.
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

// RedirectURIMatches reports whether the request's redirect_uri matches an
// entry an authorization server holds for the client. The rule is exact
// string matching (RFC 9700 §4.1.3) with exactly one exception, enabled by
// allowLoopbackPortVariance: when both the registered and requested URIs are
// RFC 8252 §7.3 loopback redirects, the port is ignored.
//
// RFC 8252 requires the AS to allow variable loopback ports for native apps —
// Claude Code binds an OS-assigned ephemeral port per invocation — and RFC
// 9700 preserves that carve-out. The port is the ONLY component allowed to
// vary: every other component must match in escaped form, and neither side
// may carry userinfo — otherwise an attacker-crafted authorize URL could
// inject extra query parameters, an encoding-variant path, or browser-sent
// Basic credentials into the legitimate client's local callback.
//
// Callers decide which clients earn the exception. Both authorization servers
// grant it to CIMD-resolved clients only: a document served over HTTPS from
// the client_id's own origin is a live statement of the vendor's redirect
// URIs, whereas a DCR registration is whatever an unauthenticated caller
// posted once, so DCR clients keep byte-exact matching.
func RedirectURIMatches(registered []string, requested string, allowLoopbackPortVariance bool) bool {
	if slices.Contains(registered, requested) {
		return true
	}
	if !allowLoopbackPortVariance {
		return false
	}

	// Fragments disqualify the exception on either side: RFC 6749 §3.1.2
	// forbids fragments in redirect URIs, and url.Parse cannot distinguish
	// an absent fragment from an explicit empty one ("...#") — URL.String()
	// drops the latter, which would let a malformed registered URI match a
	// fragment-less request. A raw-string check is the only reliable guard.
	if strings.Contains(requested, "#") {
		return false
	}
	requestedURL, err := url.Parse(requested)
	if err != nil || requestedURL.User != nil || !IsLoopbackRedirectURI(requestedURL) {
		return false
	}
	for _, entry := range registered {
		if strings.Contains(entry, "#") {
			continue
		}
		registeredURL, err := url.Parse(entry)
		if err != nil || registeredURL.User != nil || !IsLoopbackRedirectURI(registeredURL) {
			continue
		}
		if loopbackRedirectEqualIgnoringPort(registeredURL, requestedURL) {
			return true
		}
	}
	return false
}

// loopbackRedirectEqualIgnoringPort reports whether two parsed loopback
// redirect URIs are identical except for the port. Rebuilding each URI with
// the port stripped and the host lowercased, then comparing the resulting
// strings, covers every remaining component in escaped form — scheme, host,
// path, and query — so a percent-encoding variant (e.g. /%63allback for a
// registered /callback) cannot slip through the variable-port exception.
// Callers must have rejected fragments on both sides already: URL.String()
// cannot represent an explicit empty fragment.
func loopbackRedirectEqualIgnoringPort(a, b *url.URL) bool {
	stripPort := func(u *url.URL) string {
		c := *u
		c.Host = strings.ToLower(c.Hostname())
		return c.String()
	}
	return stripPort(a) == stripPort(b)
}

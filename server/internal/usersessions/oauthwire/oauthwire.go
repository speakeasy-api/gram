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

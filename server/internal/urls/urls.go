// Package urls holds validation helpers for URLs that arrive from outside
// Gram — request payloads, upstream metadata documents, and other untrusted
// sources — and that Gram stores, dials, or renders as a link.
package urls

import (
	"net"
	"net/url"
)

// IsAbsoluteHTTP reports whether raw is an absolute http(s) URL carrying a
// host.
//
// url.Parse alone is not a validation: it accepts "javascript:alert(1)",
// "mailto:x@example.com", and bare relative strings like "docs" without error.
// Callers that persist a URL or emit it into an href need the scheme and host
// checked too, which is what this does.
func IsAbsoluteHTTP(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// IsAbsoluteHTTPS reports whether raw is an absolute HTTPS URL carrying a host.
//
// Use this when a URL will carry sensitive data (tokens, credentials) that must
// not be transmitted in plaintext. The stricter constraint rejects http://
// schemes that IsAbsoluteHTTP would accept.
func IsAbsoluteHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	return u.Scheme == "https" && u.Host != ""
}

// IsAbsoluteHTTPSOrLoopback reports whether raw is an absolute URL that Gram may
// send credentials to: HTTPS to any host, or plain HTTP to loopback.
//
// The loopback exemption does not weaken the guarantee IsAbsoluteHTTPS exists
// for. A token sent to 127.0.0.1 never crosses a network, so there is no
// plaintext transmission to intercept — the same line RFC 8252 §8.3 draws for
// native-app redirect URIs. It is also unreachable in production: guardian's
// default egress policy blocks 127.0.0.0/8 and ::1/128, so a loopback endpoint
// is refused before any request is made, whatever this returns.
//
// What it buys is local development and tests against an http:// identity
// provider — the dev-idp harness advertises its endpoints on plain loopback —
// without either weakening production or maintaining a TLS fixture for every
// upstream a test stands up.
func IsAbsoluteHTTPSOrLoopback(raw string) bool {
	if IsAbsoluteHTTPS(raw) {
		return true
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return false
	}

	host := u.Hostname()
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

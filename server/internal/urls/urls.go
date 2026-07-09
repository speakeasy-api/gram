// Package urls holds validation helpers for URLs that arrive from outside
// Gram — request payloads, upstream metadata documents, and other untrusted
// sources — and that Gram stores, dials, or renders as a link.
package urls

import "net/url"

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

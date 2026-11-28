// Package emailnorm canonicalizes email addresses for duplicate-account and
// blocklist matching, collapsing Gmail and Googlemail alias variations.
package emailnorm

import "strings"

const (
	gmailDomain      = "gmail.com"
	googlemailDomain = "googlemail.com"
)

// Canonicalize returns the canonical form of email for identity comparison.
//
// It trims surrounding whitespace and lowercases the address. For gmail.com
// and googlemail.com addresses, it maps googlemail.com to gmail.com, strips
// any +tag suffix from the local part, and removes all dots from the local
// part. Non-Gmail domains keep their local part intact.
//
// Malformed addresses (missing or repeated @, empty local or domain part) and
// degenerate Gmail addresses whose local part would become empty after
// stripping dots and +tags (such as "...+tag@gmail.com") are returned trimmed
// and lowercased without panicking so distinct malformed values do not collide.
func Canonicalize(email string) string {
	trimmed := strings.ToLower(strings.TrimSpace(email))
	local, domain, ok := strings.Cut(trimmed, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return trimmed
	}

	if domain == googlemailDomain {
		domain = gmailDomain
	}
	if domain != gmailDomain {
		return trimmed
	}

	if base, _, hasPlus := strings.Cut(local, "+"); hasPlus {
		local = base
	}
	local = strings.ReplaceAll(local, ".", "")
	if local == "" {
		return trimmed
	}

	return local + "@" + domain
}

// IsGmailDomain reports whether email is a well-formed gmail.com or
// googlemail.com address whose canonical local part is non-empty.
func IsGmailDomain(email string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(email))
	local, domain, ok := strings.Cut(trimmed, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}
	if domain != gmailDomain && domain != googlemailDomain {
		return false
	}

	if base, _, hasPlus := strings.Cut(local, "+"); hasPlus {
		local = base
	}
	return strings.ReplaceAll(local, ".", "") != ""
}

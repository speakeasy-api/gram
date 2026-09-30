// Package oktaissuer holds the Okta issuer URL rule shared by the registry
// catalog mapping and the resource connection audience. It imports nothing
// from the server so both sides can use it.
package oktaissuer

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// ErrInvalid is returned for every rejected issuer; the text never echoes input.
var ErrInvalid = errors.New("https URL with a host and without query, fragment or userinfo required")

// Validate applies the Okta resource connection audience rule: https, a host,
// an optional path, nothing else. Okta compares audiences byte for byte, so no
// normalization happens here.
func Validate(issuer string) error {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.ForceQuery || strings.ContainsAny(issuer, "?#") || HasInvisible(issuer) {
		return ErrInvalid
	}
	return nil
}

// HasInvisible reports whitespace, control and format characters, including
// the non-ASCII ones a JSON Schema \s class misses; a pasted no-break space
// would otherwise mint a key that never matches an Okta name.
func HasInvisible(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) >= 0
}

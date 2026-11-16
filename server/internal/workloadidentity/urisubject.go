package workloadidentity

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// uriSubjectSchemes are the subject schemes whose syntax is checked on write.
// Every other subject format (GitHub's `repo:…`, Kubernetes'
// `system:serviceaccount:…`, AWS ARNs, Google's numeric ids) stays opaque.
var uriSubjectSchemes = []string{"wimse", "spiffe"}

// ErrURISubjectMalformed reports a wimse:// or spiffe:// subject that breaks
// draft-ietf-wimse-identifier: it must be an absolute URI with a non-empty
// authority and no user information, port, query or fragment. A conformant
// issuer never signs such a subject, so a rule naming one matches nothing.
var ErrURISubjectMalformed = errors.New("malformed workload identifier")

// validateURISubject checks a wimse:// or spiffe:// subject, or a wildcard
// rule's stem, against draft-ietf-wimse-identifier. A stem is a leading
// portion of an identifier, so it is checked only as far as it goes: it needs
// an authority, and nothing in it may be a part the full identifier forbids.
// The subject is still stored and compared exactly as written.
func validateURISubject(value string) error {
	scheme, _, found := strings.Cut(value, ":")
	if !found || !isURISubjectScheme(scheme) {
		return nil
	}

	if !strings.HasPrefix(value[len(scheme)+1:], "//") {
		return fmt.Errorf("%w: %s:// is required", ErrURISubjectMalformed, strings.ToLower(scheme))
	}
	// Checked on the raw value, because url.Parse drops an empty query or
	// fragment marker that the issuer would still have to sign.
	if strings.ContainsAny(value, "?#") {
		return fmt.Errorf("%w: a query or fragment is not allowed", ErrURISubjectMalformed)
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrURISubjectMalformed, err)
	}

	switch {
	case parsed.Host == "":
		return fmt.Errorf("%w: a trust domain is required after %s://", ErrURISubjectMalformed, strings.ToLower(scheme))
	case parsed.User != nil:
		return fmt.Errorf("%w: user information is not allowed", ErrURISubjectMalformed)
	case strings.Contains(parsed.Host, ":"):
		return fmt.Errorf("%w: a port is not allowed", ErrURISubjectMalformed)
	default:
		return nil
	}
}

func isURISubjectScheme(scheme string) bool {
	for _, known := range uriSubjectSchemes {
		if strings.EqualFold(scheme, known) {
			return true
		}
	}
	return false
}

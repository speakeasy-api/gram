package identityproviderconnections

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// oktaOrgHostSuffixes are the Okta-owned domains an org URL may live under.
// Custom domains are only admitted through a platform admin override.
var oktaOrgHostSuffixes = []string{"okta.com", "oktapreview.com", "okta-emea.com", "okta.mil"}

var (
	ErrOrgURLInvalid        = errors.New("org url is not a valid URL")
	ErrOrgURLNotHTTPS       = errors.New("org url must use https")
	ErrOrgURLNotOrigin      = errors.New("org url must be a bare origin without path, query, fragment, userinfo, or port")
	ErrOrgURLHostNotAllowed = errors.New("org url host must be a subdomain of an Okta-owned domain")
	ErrOrgURLHostNotASCII   = errors.New("org url host must be a plain ASCII hostname")
)

// NormalizeOktaOrgURL validates an administrator-supplied Okta org URL and
// returns its canonical origin form. The value becomes the issuer Gram
// discovers and the audience it signs client assertions for, so anything
// beyond an https origin on an Okta-owned host is refused.
func NormalizeOktaOrgURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ErrOrgURLInvalid
	}
	if parsed.Scheme != "https" {
		return "", ErrOrgURLNotHTTPS
	}
	// A bare trailing ? or # parses to an empty query or fragment; refuse them too.
	if parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") || parsed.Port() != "" {
		return "", ErrOrgURLNotOrigin
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", ErrOrgURLNotOrigin
	}
	if parsed.Host == "" || parsed.Host != parsed.Hostname() {
		return "", ErrOrgURLNotOrigin
	}
	// Checked before lowercasing: case folding maps some non-ASCII runes (the
	// Kelvin sign) onto ASCII letters.
	if !isASCII(parsed.Hostname()) {
		return "", ErrOrgURLHostNotASCII
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(host, ".") || !isPlainHostname(host) {
		return "", ErrOrgURLHostNotASCII
	}
	for _, suffix := range oktaOrgHostSuffixes {
		if !strings.HasSuffix(host, "."+suffix) {
			continue
		}
		tenant := strings.TrimSuffix(host, "."+suffix)
		// Administrators often paste the admin console address, which is the
		// org hostname with -admin appended; the org itself is what Okta
		// reports as the issuer.
		tenant = strings.TrimSuffix(tenant, "-admin")
		if tenant == "" {
			break
		}
		return "https://" + tenant + "." + suffix, nil
	}
	return "", ErrOrgURLHostNotAllowed
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// isPlainHostname admits lowercase LDH labels only: no punycode, no unicode.
func isPlainHostname(host string) bool {
	for label := range strings.SplitSeq(host, ".") {
		if label == "" || strings.HasPrefix(label, "xn--") || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

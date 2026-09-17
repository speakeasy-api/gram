package identityproviderconnections

import (
	"errors"
	"net/url"
	"strings"
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
	if parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return "", ErrOrgURLNotOrigin
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", ErrOrgURLNotOrigin
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || parsed.Host != parsed.Hostname() {
		return "", ErrOrgURLNotOrigin
	}
	if strings.HasSuffix(host, ".") || !isPlainHostname(host) {
		return "", ErrOrgURLHostNotASCII
	}
	allowed := false
	for _, suffix := range oktaOrgHostSuffixes {
		if strings.HasSuffix(host, "."+suffix) && len(host) > len(suffix)+1 {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", ErrOrgURLHostNotAllowed
	}
	return "https://" + host, nil
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

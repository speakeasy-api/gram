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
	ErrOrgURLNotOrigin      = errors.New("org url must be an origin or admin console url without query, fragment, userinfo, or port")
	ErrOrgURLHostNotAllowed = errors.New("org url host must be a subdomain of an Okta-owned domain")
	ErrOrgURLHostNotASCII   = errors.New("org url host must be a plain ASCII hostname")
)

// NormalizeOktaOrgURL validates an administrator-supplied Okta org URL and
// returns its canonical origin form. The value becomes the issuer Gram
// discovers and the audience it signs client assertions for, so anything
// beyond an https origin on an Okta-owned host is refused, except an admin
// console URL (acme-admin.okta.com/admin/...), which resolves to its org.
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
	tenant, suffix := "", ""
	for _, s := range oktaOrgHostSuffixes {
		if t, ok := strings.CutSuffix(host, "."+s); ok && t != "" {
			tenant, suffix = t, s
			break
		}
	}
	if tenant == "" {
		return "", ErrOrgURLHostNotAllowed
	}
	// Matching on the decoded path would admit encoded spellings of /admin.
	if strings.Contains(parsed.EscapedPath(), "%") {
		return "", ErrOrgURLNotOrigin
	}
	// The console host adds -admin to the label next to the Okta suffix.
	org, isAdminHost := strings.CutSuffix(tenant, "-admin")
	isAdminHost = isAdminHost && org != "" && !strings.HasSuffix(org, "-") && !strings.HasSuffix(org, ".")
	path := parsed.Path
	if isAdminHost {
		// Stripping twice would name a tenant the admin never typed.
		if strings.HasSuffix(org, "-admin") {
			return "", ErrOrgURLNotOrigin
		}
		host = org + "." + suffix
		if path == "/admin" || strings.HasPrefix(path, "/admin/") {
			path = ""
		}
	}
	if path != "" && path != "/" {
		return "", ErrOrgURLNotOrigin
	}
	return "https://" + host, nil
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

package remotesessions

import (
	"errors"
	"net/url"
	"strings"
)

// EMAProviderKind names a source of ID-JAGs for identity chaining.
type EMAProviderKind string

const (
	// EMAProviderExternal is an organization identity provider connection,
	// such as Okta, that exchanges the human's ID token for an ID-JAG.
	EMAProviderExternal EMAProviderKind = "external"

	// EMAProviderSpeakeasy is Speakeasy's own per-organization issuer.
	EMAProviderSpeakeasy EMAProviderKind = "speakeasy"
)

// ParseEMAProviderKind accepts only the known provider kinds.
func ParseEMAProviderKind(s string) (EMAProviderKind, bool) {
	switch kind := EMAProviderKind(s); kind {
	case EMAProviderExternal, EMAProviderSpeakeasy:
		return kind, true
	}
	return "", false
}

// ErrInvalidVendorIssuer marks a vendor issuer that is not an absolute HTTP(S) URL.
var ErrInvalidVendorIssuer = errors.New("remotesessions: vendor issuer must be an absolute http(s) URL")

// NormalizeVendorIssuer keys a vendor's per-organization provider policy by
// its RFC 8414 issuer: lowercase scheme and host, no trailing slash, no query
// or fragment. Path case is preserved.
func NormalizeVendorIssuer(issuer string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalidVendorIssuer
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", ErrInvalidVendorIssuer
	}
	return scheme + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.EscapedPath(), "/"), nil
}

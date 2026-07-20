package shadowmcp

import (
	"net"
	"net/url"
	"strings"
)

type InventoryURL struct {
	CanonicalURL string
	URLHost      string
}

func CanonicalizeInventoryURL(raw string) (InventoryURL, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return InventoryURL{
			CanonicalURL: "",
			URLHost:      "",
		}, false
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return InventoryURL{
			CanonicalURL: "",
			URLHost:      "",
		}, false
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	normalizedHost := NormalizeURLHost(parsed.Scheme, parsed.Host)
	parsed.Host = normalizedHost
	if strings.Contains(normalizedHost, ":") && net.ParseIP(normalizedHost) != nil {
		parsed.Host = "[" + normalizedHost + "]"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	sanitized := parsed.String()

	canonical, err := NormalizeMatchValue(MatchBreadthFullURL, sanitized)
	if err != nil {
		return InventoryURL{
			CanonicalURL: "",
			URLHost:      "",
		}, false
	}

	return InventoryURL{
		CanonicalURL: canonical,
		URLHost:      normalizedHost,
	}, true
}

func AccessEvidenceForInventoryURL(value InventoryURL) AccessEvidence {
	return AccessEvidence{
		FullURL:        value.CanonicalURL,
		URLHost:        value.URLHost,
		ServerIdentity: "",
	}
}

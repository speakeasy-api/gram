package shadowmcp

import (
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
	parsed.Host = NormalizeURLHost(parsed.Scheme, parsed.Host)
	parsed.User = nil
	parsed.RawQuery = ""
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
		URLHost:      NormalizeURLHost(parsed.Scheme, parsed.Host),
	}, true
}

func AccessEvidenceForInventoryURL(value InventoryURL) AccessEvidence {
	return AccessEvidence{
		FullURL:        value.CanonicalURL,
		URLHost:        value.URLHost,
		ServerIdentity: "",
	}
}

package shadowmcp

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/conv"
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

// ServerSlug is the URL-safe page identifier for one inventory server: a
// readable prefix from the canonical URL plus an eight-character hash suffix
// that survives prefix collisions. The inventory pages and the approval
// workflow both derive it from the same canonical URL, which is what lets a
// request link to the server page it describes.
func ServerSlug(canonicalURL string) string {
	hash := sha256.Sum256([]byte(canonicalURL))
	hashSuffix := hex.EncodeToString(hash[:])[:8]

	label := strings.TrimPrefix(canonicalURL, "https://")
	label = strings.TrimPrefix(label, "http://")
	prefix := conv.URLToSlug(label)
	if prefix == "" {
		prefix = "server"
	}

	return prefix + "-" + hashSuffix
}

package plugins

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// marketplaceTokenBytes sets the entropy size for the URL-as-secret token. 32
// bytes (256 bits) is well past the threshold for resisting offline guessing
// even at internet scale; the base64url-without-padding encoding lands at
// exactly 43 chars from the [A-Za-z0-9_-] set — the marketplace proxy's
// handler precheck pins that shape via validTokenPattern.
const marketplaceTokenBytes = 32

// generateMarketplaceToken returns a fresh URL-safe opaque token used by the
// marketplace proxy. The token is the URL-as-secret credential — it carries no
// project identity on its own; resolution happens server-side through the
// plugin_github_connections.marketplace_token index.
func generateMarketplaceToken() (string, error) {
	buf := make([]byte, marketplaceTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

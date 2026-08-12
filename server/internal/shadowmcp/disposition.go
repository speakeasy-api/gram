package shadowmcp

import "slices"

// Default dispositions for shadow MCP blocking policies. DispositionBlockAll
// denies every non-Gram-hosted server unless explicitly allowed (the original
// behavior); DispositionAllowAll permits every server unless it appears on
// the policy's blocked-URL list.
//
// Must stay in sync with RiskPolicyShadowMCPDispositionEnum in
// server/design/shared/risk.go — the design package cannot import this one.
const (
	DispositionBlockAll = "block_all"
	DispositionAllowAll = "allow_all"
)

// BlockedURLMatch reports whether fullURL canonicalizes to an entry in
// blockedURLs — the canonical blocked-URL set of an allow_all policy. A URL
// that cannot be canonicalized never matches: under allow-all the default is
// to permit, so unrecognizable evidence falls through to an allow.
func BlockedURLMatch(blockedURLs []string, fullURL string) (string, bool) {
	if fullURL == "" || len(blockedURLs) == 0 {
		return "", false
	}
	inventoryURL, ok := CanonicalizeInventoryURL(fullURL)
	if !ok {
		return "", false
	}
	if slices.Contains(blockedURLs, inventoryURL.CanonicalURL) {
		return inventoryURL.CanonicalURL, true
	}
	return "", false
}

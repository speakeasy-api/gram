package aitargets

import "slices"

// GatewayCaller is what an MCP gateway request proved about the client behind
// it. Every field is optional: an OAuth bearer carries a verified client_id, a
// CIMD-admitted one also resolves to a catalog entry, and an API-key caller
// carries no client identity at all.
type GatewayCaller struct {
	// OAuthClientID is the client_id verified on the bearer.
	OAuthClientID string

	// CIMDVendorKey is the vendor key of the catalog entry that admitted
	// OAuthClientID. Empty for a dynamically registered client.
	CIMDVendorKey string

	// CIMDCatalogURL is that entry's URL, possibly a wildcard pattern.
	CIMDCatalogURL string
}

// MatchGatewayCaller resolves a caller to the served target whose matchers
// claim it, by verified client_id, then admitting catalog URL, then vendor key.
//
// CIMD only, enforced in the data rather than here: validateGatewayClient
// holds OAuthClientIDs to https URLs, which is what a CIMD client_id is and
// what a dynamically registered client's opaque id is not. So the literal
// client_id layer runs for every verified caller, including one admitted by an
// issuer's own CIMD entry rather than the compile-time catalog — those resolve
// to no preset, and requiring one here would have made a block on them inert.
// The catalog layers below still need their own values and skip themselves
// when the caller has none.
//
// Verified credentials are the only input. A client's self-reported name at
// initialize is never consulted and there is deliberately no matcher for it:
// any client may claim any name, so resolving a target that way would let a
// caller pick which access decision it is judged under.
func MatchGatewayCaller(targets []Target, caller GatewayCaller) (Target, bool) {
	if caller.OAuthClientID == "" {
		return ZeroTarget(), false
	}
	for _, layer := range []struct {
		value  string
		lookup func(Target) []string
	}{
		{value: caller.OAuthClientID, lookup: func(t Target) []string { return t.GatewayClient.OAuthClientIDs }},
		{value: caller.CIMDCatalogURL, lookup: func(t Target) []string { return t.GatewayClient.OAuthClientIDs }},
		{value: caller.CIMDVendorKey, lookup: func(t Target) []string { return t.GatewayClient.CIMDVendorKeys }},
	} {
		if layer.value == "" {
			continue
		}
		for _, target := range targets {
			if slices.Contains(layer.lookup(target), layer.value) {
				return target.Clone(), true
			}
		}
	}
	return ZeroTarget(), false
}

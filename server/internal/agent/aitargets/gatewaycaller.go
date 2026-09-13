package aitargets

import (
	"slices"
	"strings"
)

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
// CIMD only. A dynamically registered id is minted per install, so naming one
// would block a single laptop rather than a product. Self-reported client
// names are not consulted; MatchGatewayClientInfo is that door.
func MatchGatewayCaller(targets []Target, caller GatewayCaller) (Target, bool) {
	if caller.OAuthClientID == "" || caller.CIMDCatalogURL == "" {
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
			if target.Enabled && slices.Contains(layer.lookup(target), layer.value) {
				return target.Clone(), true
			}
		}
	}
	return ZeroTarget(), false
}

// MatchGatewayClientInfo resolves a self-reported client name to a served
// target. Evidence, never permission. Case-insensitive: the casing varies
// between a client's own releases.
func MatchGatewayClientInfo(targets []Target, clientInfoName string) (Target, bool) {
	if clientInfoName == "" {
		return ZeroTarget(), false
	}
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		for _, name := range target.GatewayClient.ClientInfoNames {
			if strings.EqualFold(name, clientInfoName) {
				return target.Clone(), true
			}
		}
	}
	return ZeroTarget(), false
}

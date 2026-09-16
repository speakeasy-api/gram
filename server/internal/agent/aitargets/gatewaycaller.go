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

// MatchReportedClientName resolves the target whose ClientInfoNames claim a
// name an MCP client reported about itself at initialize, matched without
// regard to case.
//
// This is the post-authentication layer, and everything about it is weaker
// than MatchGatewayCaller above: the name is self-reported, so any client may
// claim any name, and one that lies is not resolved at all.
//
// It is safe despite that because of what the caller does with the answer: the
// result is only ever used to REFUSE. A name never admits anyone and never
// upgrades a decision, so lying can lose a caller access it would otherwise
// have had, or fail to save it, and can never gain it access that the verified
// layer would have denied. Spoofing is an evasion risk, not an escalation one.
//
// Which is why this must never become an input to MatchGatewayCaller, whose
// answer decides an approval as well as a block. The two are kept apart
// deliberately, and the ordering is the whole safety argument: a verified
// credential is resolved first and decides, and this runs afterwards against
// what is left.
func MatchReportedClientName(targets []Target, reportedName string) (Target, bool) {
	if reportedName == "" {
		return ZeroTarget(), false
	}
	for _, target := range targets {
		for _, claimed := range target.GatewayClient.ClientInfoNames {
			if strings.EqualFold(claimed, reportedName) {
				return target.Clone(), true
			}
		}
	}
	return ZeroTarget(), false
}

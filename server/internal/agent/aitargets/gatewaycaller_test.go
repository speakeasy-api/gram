package aitargets_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// gatewayTarget is a minimal target carrying one gateway-client block.
func gatewayTarget(id string, gateway aitargets.GatewayClient) aitargets.Target {
	return aitargets.Target{
		ID:            id,
		DisplayName:   id,
		Category:      aitargets.CategoryHarness,
		Signatures:    aitargets.Signatures{BundleIDs: nil, Binaries: []string{id}, ConfigDirs: nil, ProcessNames: nil},
		VersionHint:   nil,
		GatewayClient: gateway,
	}
}

func TestMatchGatewayCallerPrefersLiteralClientIDOverCatalogEntry(t *testing.T) {
	t.Parallel()

	// The reason the layers exist: OpenAI's connector wildcard also admits
	// Codex's stable document, so without literal-first resolution a Codex
	// caller would answer to the ChatGPT target.
	targets := []aitargets.Target{
		gatewayTarget("chatgpt", aitargets.GatewayClient{
			CIMDVendorKeys:  []string{"openai-connectors"},
			OAuthClientIDs:  []string{"https://chatgpt.com/oauth/*/client.json"},
			ClientInfoNames: nil,
		}),
		gatewayTarget("codex", aitargets.GatewayClient{
			CIMDVendorKeys:  nil,
			OAuthClientIDs:  []string{"https://chatgpt.com/oauth/codex/client.json"},
			ClientInfoNames: nil,
		}),
	}

	matched, ok := aitargets.MatchGatewayCaller(targets, aitargets.GatewayCaller{
		OAuthClientID:  "https://chatgpt.com/oauth/codex/client.json",
		CIMDVendorKey:  "openai-connectors",
		CIMDCatalogURL: "https://chatgpt.com/oauth/*/client.json",
	})
	require.True(t, ok)
	require.Equal(t, "codex", matched.ID)
}

func TestMatchGatewayCallerMatchesCatalogURLThenVendorKey(t *testing.T) {
	t.Parallel()

	byURL := gatewayTarget("codex", aitargets.GatewayClient{
		CIMDVendorKeys:  nil,
		OAuthClientIDs:  []string{"https://chatgpt.com/oauth/codex/*/client.json"},
		ClientInfoNames: nil,
	})
	byVendor := gatewayTarget("claude-code", aitargets.GatewayClient{
		CIMDVendorKeys:  []string{"anthropic"},
		OAuthClientIDs:  nil,
		ClientInfoNames: nil,
	})
	targets := []aitargets.Target{byURL, byVendor}

	// A per-server client_id repeats for nobody, so only the catalog pattern
	// can name it.
	matched, ok := aitargets.MatchGatewayCaller(targets, aitargets.GatewayCaller{
		OAuthClientID:  "https://chatgpt.com/oauth/codex/abc123/client.json",
		CIMDVendorKey:  "openai",
		CIMDCatalogURL: "https://chatgpt.com/oauth/codex/*/client.json",
	})
	require.True(t, ok)
	require.Equal(t, "codex", matched.ID)

	matched, ok = aitargets.MatchGatewayCaller(targets, aitargets.GatewayCaller{
		OAuthClientID:  "https://claude.ai/oauth/claude-code-client-metadata",
		CIMDVendorKey:  "anthropic",
		CIMDCatalogURL: "https://claude.ai/oauth/claude-code-client-metadata",
	})
	require.True(t, ok)
	require.Equal(t, "claude-code", matched.ID)
}

// A CIMD client_id is the https URL its document is served from, and that is
// the only shape validateGatewayClient lets into OAuthClientIDs — a
// dynamically registered client's opaque id cannot be written there at all. So
// the literal layer does not need a catalog entry to be safe, and requiring
// one would have made a block inert for exactly the clients an organization
// admitted itself: an issuer's own CIMD entry resolves to no compile-time
// preset.
func TestMatchGatewayCallerMatchesAnIssuerAdmittedClientWithNoPreset(t *testing.T) {
	t.Parallel()

	targets := []aitargets.Target{gatewayTarget("acme-tool", aitargets.GatewayClient{
		CIMDVendorKeys:  nil,
		OAuthClientIDs:  []string{"https://acme.example/mcp/client.json"},
		ClientInfoNames: nil,
	})}

	target, ok := aitargets.MatchGatewayCaller(targets, aitargets.GatewayCaller{
		OAuthClientID:  "https://acme.example/mcp/client.json",
		CIMDVendorKey:  "",
		CIMDCatalogURL: "",
	})
	require.True(t, ok, "a verified client_id matches its literal matcher without a catalog preset")
	require.Equal(t, "acme-tool", target.ID)

	_, ok = aitargets.MatchGatewayCaller(targets, aitargets.GatewayCaller{
		OAuthClientID:  "https://other.example/mcp/client.json",
		CIMDVendorKey:  "",
		CIMDCatalogURL: "",
	})
	require.False(t, ok, "a different document is a different client")
}

// TestMatchGatewayCallerIgnoresUnverifiedCallers: identity at the gateway is
// a client_id the server verified. A caller presenting none is not identified
// at all, whatever the catalog says about the vendor key it claims, so it
// resolves to no target and no decision can be enforced against it.
func TestMatchGatewayCallerIgnoresUnverifiedCallers(t *testing.T) {
	t.Parallel()

	cursor := gatewayTarget("cursor", aitargets.GatewayClient{
		CIMDVendorKeys:  []string{"cursor"},
		OAuthClientIDs:  nil,
		ClientInfoNames: []string{"Cursor"},
	})

	_, ok := aitargets.MatchGatewayCaller([]aitargets.Target{cursor}, aitargets.GatewayCaller{
		OAuthClientID:  "",
		CIMDVendorKey:  "cursor",
		CIMDCatalogURL: "",
	})
	require.False(t, ok, "an unverified caller is never matched")

	// The same caller carrying a verified client_id does match on the vendor
	// key, so the refusal above is the missing client_id rather than the
	// target being unreachable.
	matched, ok := aitargets.MatchGatewayCaller([]aitargets.Target{cursor}, aitargets.GatewayCaller{
		OAuthClientID:  "https://cursor.com/mcp/client.json",
		CIMDVendorKey:  "cursor",
		CIMDCatalogURL: "https://cursor.com/mcp/client.json",
	})
	require.True(t, ok, "a verified caller carrying the vendor key resolves")
	require.Equal(t, "cursor", matched.ID)
}

// firstBlockableVendorKey borrows a vendor key the registry says the gateway
// can resolve. Which vendor that is moves as the registry grows.
func firstBlockableVendorKey() (string, bool) {
	for _, target := range aitargets.Defaults() {
		for _, key := range target.GatewayClient.CIMDVendorKeys {
			return key, true
		}
	}
	return "", false
}

func TestValidateRejectsTwoTargetsClaimingOneMatcher(t *testing.T) {
	t.Parallel()

	// A key only one target may claim, so the failure under test is the
	// duplicate rather than the key itself: `anthropic` covers two products
	// and validateGatewayClient turns it away before this check is reached.
	shared, found := firstBlockableVendorKey()
	require.True(t, found)

	first := gatewayTarget("claude-code", aitargets.GatewayClient{
		CIMDVendorKeys:  []string{shared},
		OAuthClientIDs:  nil,
		ClientInfoNames: nil,
	})
	second := gatewayTarget("claude-desktop", aitargets.GatewayClient{
		CIMDVendorKeys:  []string{shared},
		OAuthClientIDs:  nil,
		ClientInfoNames: nil,
	})

	err := aitargets.Validate([]aitargets.Target{first, second})
	require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
	require.Contains(t, err.Error(), "CIMD vendor key")

	// Dropping one resolves the ambiguity. Leaving the inventory is the only
	// way a target stops being matched against a caller, so it is also the
	// only way this collision clears.
	require.NoError(t, aitargets.Validate([]aitargets.Target{first}))
}

func TestGatewayMatchersStayOutOfTheAgentEnvelope(t *testing.T) {
	t.Parallel()

	bare := gatewayTarget("claude-code", aitargets.GatewayClient{CIMDVendorKeys: nil, OAuthClientIDs: nil, ClientInfoNames: nil})
	linked := gatewayTarget("claude-code", aitargets.GatewayClient{
		CIMDVendorKeys:  []string{"anthropic"},
		OAuthClientIDs:  []string{"https://claude.ai/oauth/claude-code-client-metadata"},
		ClientInfoNames: []string{"claude-code"},
	})

	bareSnapshot := aitargets.NewSnapshot(7, []aitargets.Target{bare})
	linkedSnapshot := aitargets.NewSnapshot(7, []aitargets.Target{linked})

	// Matchers are server-side only: agents must not receive them, and a
	// matcher-only edit must not churn the ETag into a needless re-apply.
	require.Equal(t, bareSnapshot.ETag, linkedSnapshot.ETag)

	encoded, err := json.Marshal(linkedSnapshot.Envelope())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "gateway_client")
	require.NotContains(t, string(encoded), "anthropic")

	// The server-side view keeps them.
	served := linkedSnapshot.Targets()
	require.Equal(t, []string{"anthropic"}, served[0].GatewayClient.CIMDVendorKeys)
}

// TestDefaultsNeverClaimAMultiProductVendorKey is the invariant the OpenAI
// case was one instance of: a vendor key that covers more than one product
// cannot name either, or blocking one would silently take the other.
//
// Anthropic is the case the hand-written matchers got wrong before the
// registry derived this — `anthropic` covers Claude Code and Claude, so
// blocking the CLI was also blocking the chat app.
func TestDefaultsNeverClaimAMultiProductVendorKey(t *testing.T) {
	t.Parallel()

	productsPerKey := map[string]int{}
	for _, product := range aivendors.Products() {
		if product.SpeaksCIMD() {
			productsPerKey[product.VendorKey]++
		}
	}

	for _, target := range aitargets.Defaults() {
		for _, key := range target.GatewayClient.CIMDVendorKeys {
			require.Equal(t, 1, productsPerKey[key],
				"target %q claims vendor key %q, which covers %d products",
				target.ID, key, productsPerKey[key])
		}
	}

	// The two known multi-product vendors, named so a regression reads clearly.
	for _, key := range []string{"openai", "anthropic"} {
		require.Greater(t, productsPerKey[key], 1, "expected %q to cover several products", key)
		for _, target := range aitargets.Defaults() {
			require.NotContains(t, target.GatewayClient.CIMDVendorKeys, key, "target %q", target.ID)
		}
	}
}

// TestEveryBlockableDefaultIsAdmissible pins the prerequisite that makes the
// registry worth having: a client absent from the CIMD admission catalog
// cannot authenticate in presets mode, so it never reaches an access
// decision. Every client id a default names must therefore be admissible.
func TestEveryBlockableDefaultIsAdmissible(t *testing.T) {
	t.Parallel()

	for _, target := range aitargets.Defaults() {
		for _, clientID := range target.GatewayClient.OAuthClientIDs {
			_, known := admission.CatalogPreset(clientID)
			require.True(t, known, "target %q names client id %q, which admission does not admit", target.ID, clientID)
		}
	}
}

// TestOverlappingVendorDocumentsDoNotCrossAttribute is the regression guard
// for a cross-product misattribution at the gateway.
//
// OpenAI publishes two products under one host. The ChatGPT connector
// wildcard admits any single path segment under /oauth/, which includes the
// stable Codex document at /oauth/codex/client.json. Both catalog entries
// therefore match that URL, and only the literal one names Codex.
//
// The catalog resolves the literal first, so the caller carries Codex's
// identity into matching. Resolving the wildcard instead would let a block on
// ChatGPT reject Codex, which is the failure this pins. The absent case is the
// one that actually regressed: with Codex out of the candidate list, a caller
// that resolved to the wildcard fell through to the surviving ChatGPT target
// instead of resolving to nothing.
//
// A target leaves the candidate list by leaving the organization's inventory —
// a built-in by being dropped from the registry, an organization's own target
// by having its row deleted. That is the single state deciding whether a
// target is probed for and matched at all, so it is what this varies.
func TestOverlappingVendorDocumentsDoNotCrossAttribute(t *testing.T) {
	t.Parallel()

	const (
		codexStable    = "https://chatgpt.com/oauth/codex/client.json"
		codexPerServer = "https://chatgpt.com/oauth/codex/AbCdEfGhI/client.json"
		chatGPTStable  = "https://chatgpt.com/oauth/client.json"
		chatGPTConn    = "https://chatgpt.com/oauth/connector-abc123/client.json"
	)

	// callerFor builds the caller exactly as the MCP gateway does, so this
	// exercises the real resolution path rather than a hand-made caller.
	callerFor := func(clientID string) aitargets.GatewayCaller {
		caller := aitargets.GatewayCaller{OAuthClientID: clientID, CIMDVendorKey: "", CIMDCatalogURL: ""}
		if preset, known := admission.CatalogPreset(clientID); known {
			caller.CIMDVendorKey = preset.VendorKey
			caller.CIMDCatalogURL = preset.URL
		}
		return caller
	}

	defaultsWithout := func(absent string) []aitargets.Target {
		defaults := aitargets.Defaults()
		targets := make([]aitargets.Target, 0, len(defaults))
		for _, target := range defaults {
			if target.ID == absent {
				continue
			}
			targets = append(targets, target)
		}
		return targets
	}

	for _, tt := range []struct {
		name     string
		absent   string
		clientID string
		want     string
	}{
		{name: "codex stable document", absent: "", clientID: codexStable, want: "codex"},
		{name: "codex per-server document", absent: "", clientID: codexPerServer, want: "codex"},
		{name: "chatgpt stable document", absent: "", clientID: chatGPTStable, want: "chatgpt"},
		{name: "chatgpt connector document", absent: "", clientID: chatGPTConn, want: "chatgpt"},

		// One product leaving the inventory must not hand its callers to the
		// other.
		{name: "codex absent, stable document", absent: "codex", clientID: codexStable, want: ""},
		{name: "codex absent, per-server document", absent: "codex", clientID: codexPerServer, want: ""},
		{name: "chatgpt absent, stable document", absent: "chatgpt", clientID: chatGPTStable, want: ""},
		{name: "chatgpt absent, codex still resolves", absent: "chatgpt", clientID: codexStable, want: "codex"},

		// Classic carries no gateway matchers: both desktop builds present
		// the same documents, so ChatGPT keeps them and a decision about
		// Classic never reaches a caller.
		{name: "classic absent, stable document still resolves to chatgpt", absent: "chatgpt-classic", clientID: chatGPTStable, want: "chatgpt"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target, matched := aitargets.MatchGatewayCaller(defaultsWithout(tt.absent), callerFor(tt.clientID))
			if tt.want == "" {
				require.Falsef(t, matched, "%q must resolve to no target, got %q", tt.clientID, target.ID)
				return
			}
			require.Truef(t, matched, "%q must resolve to a target", tt.clientID)
			require.Equal(t, tt.want, target.ID)
		})
	}
}

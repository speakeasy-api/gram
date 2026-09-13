package aitargets_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// gatewayTarget is a minimal served target carrying one gateway-client block.
func gatewayTarget(id string, gateway aitargets.GatewayClient) aitargets.Target {
	return aitargets.Target{
		ID:            id,
		DisplayName:   id,
		Category:      aitargets.CategoryHarness,
		Signatures:    aitargets.Signatures{BundleIDs: nil, Binaries: []string{id}, ConfigDirs: nil, ProcessNames: nil},
		VersionHint:   nil,
		GatewayClient: gateway,
		Enabled:       true,
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

// TestMatchGatewayCallerRequiresACatalogEntry: blocking is CIMD-only. A
// dynamically registered client gets an opaque id minted per registration, so
// naming one would block a single install and the next registration would
// walk past it.
func TestMatchGatewayCallerRequiresACatalogEntry(t *testing.T) {
	t.Parallel()

	targets := []aitargets.Target{gatewayTarget("some-tool", aitargets.GatewayClient{
		CIMDVendorKeys:  nil,
		OAuthClientIDs:  []string{"dcr-opaque-client-id"},
		ClientInfoNames: nil,
	})}

	_, ok := aitargets.MatchGatewayCaller(targets, aitargets.GatewayCaller{
		OAuthClientID:  "dcr-opaque-client-id",
		CIMDVendorKey:  "",
		CIMDCatalogURL: "",
	})
	require.False(t, ok, "a client that did not come from the CIMD catalog never matches")
}

func TestMatchGatewayCallerIgnoresUnverifiedAndDisabledTargets(t *testing.T) {
	t.Parallel()

	disabled := gatewayTarget("cursor", aitargets.GatewayClient{
		CIMDVendorKeys:  []string{"cursor"},
		OAuthClientIDs:  nil,
		ClientInfoNames: []string{"Cursor"},
	})
	disabled.Enabled = false

	// A self-reported name is never an authorization key, and a target the
	// organization does not scan for is not enforced on either.
	_, ok := aitargets.MatchGatewayCaller([]aitargets.Target{disabled}, aitargets.GatewayCaller{
		OAuthClientID:  "",
		CIMDVendorKey:  "cursor",
		CIMDCatalogURL: "",
	})
	require.False(t, ok)

	enabled := gatewayTarget("cursor", disabled.GatewayClient)
	_, ok = aitargets.MatchGatewayCaller([]aitargets.Target{enabled}, aitargets.GatewayCaller{
		OAuthClientID:  "",
		CIMDVendorKey:  "cursor",
		CIMDCatalogURL: "",
	})
	require.False(t, ok, "a caller with no verified client id never matches")
}

func TestMatchGatewayClientInfoIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	targets := []aitargets.Target{gatewayTarget("cursor", aitargets.GatewayClient{
		CIMDVendorKeys:  nil,
		OAuthClientIDs:  nil,
		ClientInfoNames: []string{"Cursor"},
	})}

	matched, ok := aitargets.MatchGatewayClientInfo(targets, "cursor")
	require.True(t, ok)
	require.Equal(t, "cursor", matched.ID)

	_, ok = aitargets.MatchGatewayClientInfo(targets, "")
	require.False(t, ok)
}

func TestValidateRejectsTwoServedTargetsClaimingOneMatcher(t *testing.T) {
	t.Parallel()

	first := gatewayTarget("claude-code", aitargets.GatewayClient{
		CIMDVendorKeys:  []string{"anthropic"},
		OAuthClientIDs:  nil,
		ClientInfoNames: nil,
	})
	second := gatewayTarget("claude-desktop", aitargets.GatewayClient{
		CIMDVendorKeys:  []string{"anthropic"},
		OAuthClientIDs:  nil,
		ClientInfoNames: nil,
	})

	err := aitargets.Validate([]aitargets.Target{first, second})
	require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
	require.Contains(t, err.Error(), "CIMD vendor key")

	// Disabling one resolves the ambiguity, because only served targets are
	// ever matched against a caller.
	second.Enabled = false
	require.NoError(t, aitargets.Validate([]aitargets.Target{first, second}))
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

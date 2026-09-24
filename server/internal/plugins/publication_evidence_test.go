package plugins

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicationFreshness(t *testing.T) {
	t.Parallel()

	current := map[string]string{
		"weather":               "plugin-fingerprint",
		mcpSharedFingerprintKey: "shared-fingerprint",
	}
	fingerprints := func(values map[string]string) []byte {
		t.Helper()
		data, err := json.Marshal(values)
		require.NoError(t, err)
		return data
	}

	tests := []struct {
		name      string
		raw       []byte
		slug      string
		current   map[string]string
		wantKnown bool
		wantFresh bool
	}{
		{name: "absent fingerprint is unknown", slug: "weather", current: current},
		{name: "malformed fingerprint is stale", raw: []byte("{"), slug: "weather", current: current, wantKnown: true},
		{name: "empty map is stale", raw: []byte(`{}`), slug: "weather", current: current, wantKnown: true},
		{name: "matching plugin and shared fingerprints are fresh", raw: fingerprints(current), slug: "weather", current: current, wantKnown: true, wantFresh: true},
		{name: "plugin mismatch is stale", raw: fingerprints(map[string]string{"weather": "old", mcpSharedFingerprintKey: "shared-fingerprint"}), slug: "weather", current: current, wantKnown: true},
		{name: "missing plugin fingerprint is stale", raw: fingerprints(map[string]string{mcpSharedFingerprintKey: "shared-fingerprint"}), slug: "weather", current: current, wantKnown: true},
		{name: "missing shared fingerprint is stale", raw: fingerprints(map[string]string{"weather": "plugin-fingerprint"}), slug: "weather", current: current, wantKnown: true},
		{name: "missing current plugin fingerprint is stale", raw: fingerprints(current), slug: "absent", current: current, wantKnown: true},
		{name: "missing current shared fingerprint is stale", raw: fingerprints(current), slug: "weather", current: map[string]string{"weather": "plugin-fingerprint"}, wantKnown: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fresh := publicationFreshness(tt.raw, tt.current, tt.slug)
			if !tt.wantKnown {
				require.Nil(t, fresh)
				return
			}
			require.NotNil(t, fresh)
			require.Equal(t, tt.wantFresh, *fresh)
		})
	}
}

func TestPublicationAddressesPreserveResolvedServerOrder(t *testing.T) {
	t.Parallel()

	info := PluginInfo{
		Slug: "weather",
		Servers: []PluginServerInfo{
			{DisplayName: "First", MCPURL: "https://one.example/mcp"},
			{DisplayName: "Second", MCPURL: "https://two.example/mcp"},
		},
	}
	require.Equal(t, []PublicationPackageAddress{
		{ServerName: "First", MCPURL: "https://one.example/mcp"},
		{ServerName: "Second", MCPURL: "https://two.example/mcp"},
	}, publicationAddresses(info))
}

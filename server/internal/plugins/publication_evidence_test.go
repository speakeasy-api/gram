package plugins

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

// Freshness is per plugin. A plugin with auto sync on is current when an
// automatic publish would push nothing new for it, its package and the shared
// manifests alike; one with auto sync off only answers for its own package, so
// another plugin's held change never makes it, or the auto-synced plugins,
// read stale.
func TestPluginFreshness(t *testing.T) {
	t.Parallel()

	infos := []PluginInfo{
		{Name: "Active", Slug: "active", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: nil},
		{Name: "Paused Current", Slug: "paused-current", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: nil},
		{Name: "Paused Held", Slug: "paused-held", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: nil},
		{Name: "Paused New", Slug: "paused-new", Description: "", Servers: nil, Skills: nil, AgentPluginsV1Issues: nil},
	}
	paused := map[string]bool{"paused-current": true, "paused-held": true, "paused-new": true}
	held := []heldPlugin{{Slug: "paused-held", Fingerprint: "held", Files: nil, Entry: sharedPluginEntry{Slug: "paused-held", Name: "", Description: "", ServerCount: 0, CodexAuthentication: "", AgentPluginsPackage: false}}}
	published := map[string]string{
		"active":                "active",
		"paused-current":        "paused-current",
		"paused-held":           "held",
		mcpSharedFingerprintKey: "shared",
	}
	automatic := maps.Clone(published)

	for _, test := range []struct {
		name      string
		automatic map[string]string
		published map[string]string
		want      map[string]bool
	}{
		{
			name:      "held and never published plugins alone wait for a manual sync",
			automatic: automatic,
			published: published,
			want:      map[string]bool{"active": true, "paused-current": true, "paused-held": false, "paused-new": false},
		},
		{
			name:      "an auto-synced plugin's own change",
			automatic: withFingerprint(automatic, "active", "edited"),
			published: published,
			want:      map[string]bool{"active": false, "paused-current": true, "paused-held": false, "paused-new": false},
		},
		{
			name:      "manifests an automatic publish would change stale the auto-synced plugins only",
			automatic: withFingerprint(automatic, mcpSharedFingerprintKey, "renamed"),
			published: published,
			want:      map[string]bool{"active": false, "paused-current": true, "paused-held": false, "paused-new": false},
		},
		{
			name:      "nothing recorded is stale",
			automatic: automatic,
			published: nil,
			want:      map[string]bool{"active": false, "paused-current": false, "paused-held": false, "paused-new": false},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, pluginFreshness(infos, paused, held, test.automatic, test.published))
		})
	}
}

func withFingerprint(fingerprints map[string]string, key, value string) map[string]string {
	out := maps.Clone(fingerprints)
	out[key] = value
	return out
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

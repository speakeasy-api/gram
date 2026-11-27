package networkingress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeConfigDefaultsDisabled(t *testing.T) {
	t.Parallel()
	var config RuntimeConfig
	require.NoError(t, config.Validate())
	require.False(t, config.MutationReady())
}

func TestRuntimeConfigCleanupDoesNotRequireApplySettings(t *testing.T) {
	t.Parallel()
	var config RuntimeConfig
	config.ReconcileTaskQueue = "gram-dev-network-ingress"
	require.NoError(t, config.Validate())
	require.False(t, config.MutationReady())
	config.ProviderMutationsEnabled = true
	require.NoError(t, config.Validate())
	require.False(t, config.MutationReady(), "apply also needs the gateway tag and slots")
}

func TestRuntimeConfigRejectsInvalidQueue(t *testing.T) {
	t.Parallel()
	config := completeRuntimeConfig()
	config.ReconcileTaskQueue = "other/queue"
	require.Error(t, config.Validate())
	require.False(t, config.MutationReady())
}

func completeRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		ReconcileTaskQueue: "gram-dev-network-ingress", ProviderMutationsEnabled: true,
		Gateway: GatewayRuntimeConfig{Tag: "tag:speakeasy", Slots: 3, APIURL: "", Namer: nil},
	}
}

func TestRuntimeConfigGatewayMutationReadiness(t *testing.T) {
	t.Parallel()
	config := completeRuntimeConfig()
	require.NoError(t, config.Validate())
	require.True(t, config.MutationReady())

	for _, test := range []struct {
		name  string
		clear func(*RuntimeConfig)
	}{
		{"tag", func(c *RuntimeConfig) { c.Gateway.Tag = "" }},
		{"slots", func(c *RuntimeConfig) { c.Gateway.Slots = 0 }},
		{"queue", func(c *RuntimeConfig) { c.ReconcileTaskQueue = "" }},
		{"mutations", func(c *RuntimeConfig) { c.ProviderMutationsEnabled = false }},
	} {
		incomplete := config
		test.clear(&incomplete)
		require.NoError(t, incomplete.Validate(), test.name)
		require.False(t, incomplete.MutationReady(), test.name)
	}

	for _, test := range []struct {
		name string
		set  func(*RuntimeConfig)
	}{
		{"bad tag", func(c *RuntimeConfig) { c.Gateway.Tag = "speakeasy" }},
		{"too many slots", func(c *RuntimeConfig) { c.Gateway.Slots = 1000 }},
		{"bad api url", func(c *RuntimeConfig) { c.Gateway.APIURL = "not a url" }},
		{"plain http api url", func(c *RuntimeConfig) { c.Gateway.APIURL = "http://api.tailscale.com" }},
	} {
		invalid := config
		test.set(&invalid)
		require.Error(t, invalid.Validate(), test.name)
		require.False(t, invalid.MutationReady(), test.name)
	}
}

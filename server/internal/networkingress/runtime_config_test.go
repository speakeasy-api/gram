package networkingress

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/k8s"
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
	config.Tailscale.OperatorNamespace = "tailscale"
	require.NoError(t, config.Validate())
	require.False(t, config.MutationReady())
	config.ProviderMutationsEnabled = true
	require.NoError(t, config.Validate())
	require.False(t, config.MutationReady())
}

func TestRuntimeConfigMutationRequiresEverySetting(t *testing.T) {
	t.Parallel()
	config := completeRuntimeConfig()
	require.NoError(t, config.Validate())
	require.True(t, config.MutationReady())
	for _, test := range []struct {
		name  string
		clear func(*RuntimeConfig)
	}{
		{"enabled", func(c *RuntimeConfig) { c.ProviderMutationsEnabled = false }},
		{"queue", func(c *RuntimeConfig) { c.ReconcileTaskQueue = "" }},
		{"operator namespace", func(c *RuntimeConfig) { c.Tailscale.OperatorNamespace = "" }},
		{"worker namespace", func(c *RuntimeConfig) { c.Tailscale.WorkerNamespace = "" }},
		{"backend namespace", func(c *RuntimeConfig) { c.Tailscale.BackendNamespace = "" }},
		{"backend labels", func(c *RuntimeConfig) { c.Tailscale.BackendPodLabels = nil }},
		{"proxy tag", func(c *RuntimeConfig) { c.Tailscale.ProxyTag = "" }},
		{"service tag", func(c *RuntimeConfig) { c.Tailscale.ServiceTag = "" }},
		{"CA secret", func(c *RuntimeConfig) { c.Tailscale.AttestorCASecret = "" }},
		{"API CIDR", func(c *RuntimeConfig) { c.Tailscale.KubernetesAPICIDR = "" }},
		{"API port", func(c *RuntimeConfig) { c.Tailscale.KubernetesAPIPort = 0 }},
		{"cluster CIDRs", func(c *RuntimeConfig) { c.Tailscale.ClusterCIDRs = nil }},
		{"attestor image", func(c *RuntimeConfig) { c.AttestorImage = "" }},
		{"backend service", func(c *RuntimeConfig) { c.BackendService = "" }},
		{"backend port", func(c *RuntimeConfig) { c.BackendPort = 0 }},
		{"ingress class", func(c *RuntimeConfig) { c.IngressClass = "" }},
		{"token audience", func(c *RuntimeConfig) { c.TokenAudience = "" }},
	} {
		incomplete := completeRuntimeConfig()
		test.clear(&incomplete)
		require.NoError(t, incomplete.Validate(), test.name)
		require.False(t, incomplete.MutationReady(), test.name)
	}
}

func TestRuntimeConfigRejectsInvalidValuesWithoutCLI(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		set  func(*RuntimeConfig)
	}{
		{"queue", func(c *RuntimeConfig) { c.ReconcileTaskQueue = "other/queue" }},
		{"operator namespace", func(c *RuntimeConfig) { c.Tailscale.OperatorNamespace = "Invalid" }},
		{"worker namespace", func(c *RuntimeConfig) { c.Tailscale.WorkerNamespace = "other/namespace" }},
		{"backend namespace", func(c *RuntimeConfig) { c.Tailscale.BackendNamespace = "other/namespace" }},
		{"backend service", func(c *RuntimeConfig) { c.BackendService = "https://service" }},
		{"CA secret", func(c *RuntimeConfig) { c.Tailscale.AttestorCASecret = "secret/name" }},
		{"backend port", func(c *RuntimeConfig) { c.BackendPort = -1 }},
		{"API port", func(c *RuntimeConfig) { c.Tailscale.KubernetesAPIPort = 65536 }},
		{"image", func(c *RuntimeConfig) { c.AttestorImage = "registry.example/gram:latest" }},
		{"label key", func(c *RuntimeConfig) { c.Tailscale.BackendPodLabels = map[string]string{"bad/key/name": "gram"} }},
		{"label value", func(c *RuntimeConfig) { c.Tailscale.BackendPodLabels = map[string]string{"app": "not valid"} }},
		{"proxy tag", func(c *RuntimeConfig) { c.Tailscale.ProxyTag = "gram-proxy" }},
		{"service tag", func(c *RuntimeConfig) { c.Tailscale.ServiceTag = "tag:" }},
		{"class", func(c *RuntimeConfig) { c.IngressClass = "nginx" }},
		{"audience", func(c *RuntimeConfig) { c.TokenAudience = "other" }},
		{"API CIDR", func(c *RuntimeConfig) { c.Tailscale.KubernetesAPICIDR = "10.0.0.1" }},
		{"cluster CIDR", func(c *RuntimeConfig) { c.Tailscale.ClusterCIDRs = []string{""} }},
	} {
		config := completeRuntimeConfig()
		test.set(&config)
		require.Error(t, config.Validate(), test.name)
		require.False(t, config.MutationReady(), test.name)
		config.ProviderMutationsEnabled = false
		require.Error(t, config.Validate(), test.name)
	}
}

func completeRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		ReconcileTaskQueue:       "gram-dev-network-ingress",
		ProviderMutationsEnabled: true,
		Tailscale: k8s.TailscaleNetworkIngressConfig{
			OperatorNamespace: "tailscale",
			WorkerNamespace:   "gram-dev",
			BackendNamespace:  "gram-dev",
			BackendPodLabels:  map[string]string{"app.kubernetes.io/name": "gram-server"},
			ProxyTag:          "tag:gram-proxy",
			ServiceTag:        "tag:gram-service",
			AttestorCASecret:  "gram-private-ca",
			KubernetesAPICIDR: "10.0.0.1/32",
			KubernetesAPIPort: 443,
			ClusterCIDRs:      []string{"10.0.0.0/8", "fd00::/8"},
		},
		AttestorImage:  "registry.example/gram@sha256:" + strings.Repeat("a", 64),
		BackendService: "gram-private",
		BackendPort:    8443,
		IngressClass:   "tailscale",
		TokenAudience:  "gram-netingress",
	}
}

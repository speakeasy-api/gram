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

func TestValidateAttestorImageReference(t *testing.T) {
	t.Parallel()
	sha256Digest := strings.Repeat("a", 64)
	require.NoError(t, ValidateAttestorImageReference("registry.example:5000/team/gram-attestor:v1@sha256:"+sha256Digest))
	for _, value := range []string{
		"registry.example/gram-attestor:latest",
		"sha256:" + sha256Digest,
		"registry.example/Bad_Name@sha256:" + sha256Digest,
		"registry.example/gram attestor@sha256:" + sha256Digest,
		"registry.example/gram-attestor@sha512:" + strings.Repeat("a", 128),
		"registry.example/gram-attestor@sha256:" + strings.Repeat("A", 64),
		"registry.example/gram-attestor@sha256:" + strings.Repeat("a", 63),
	} {
		require.Error(t, ValidateAttestorImageReference(value), value)
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

func TestRuntimeConfigGatewayMutationReadiness(t *testing.T) {
	t.Parallel()
	config := RuntimeConfig{
		ReconcileTaskQueue: "gram-dev-network-ingress", ProviderMutationsEnabled: true,
		Gateway: GatewayRuntimeConfig{Enabled: true, Tag: "tag:speakeasy", Slots: 3, APIURL: ""},
	}
	require.NoError(t, config.Validate())
	require.True(t, config.MutationReady(), "the gateway needs no operator settings")
	require.False(t, config.OperatorApplyReady())

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
	}

	// Enabling the gateway on an operator-complete configuration keeps it ready.
	both := completeRuntimeConfig()
	both.Gateway = GatewayRuntimeConfig{Enabled: true, Tag: "tag:speakeasy", Slots: 3, APIURL: "https://api.tailscale.com"}
	require.True(t, both.MutationReady())
	require.True(t, both.OperatorApplyReady())
}

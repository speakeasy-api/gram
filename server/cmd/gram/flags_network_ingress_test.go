package gram

import (
	"flag"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestNetworkIngressProviderDefaultsDisabled(t *testing.T) {
	t.Parallel()
	command := newNetworkIngressWorkerCommand()
	flag, ok := requireFlag(t, command.Flags, networkIngressMutationFlag).(*cli.BoolFlag)
	require.True(t, ok, command.Name)
	require.False(t, flag.Value, command.Name)
	require.Equal(t, []string{"GRAM_NETWORK_INGRESS_PROVIDER_MUTATIONS_ENABLED"}, flag.EnvVars)
}

func TestNetworkIngressQueueDefaultsUnconfigured(t *testing.T) {
	t.Parallel()
	for _, command := range []*cli.Command{newStartCommand(), newNetworkIngressWorkerCommand(), newStreamsCommand()} {
		flag, ok := requireFlag(t, command.Flags, networkIngressQueueFlag).(*cli.StringFlag)
		require.True(t, ok, command.Name)
		require.Empty(t, flag.Value, command.Name)
		require.Equal(t, []string{"GRAM_NETWORK_INGRESS_RECONCILE_TASK_QUEUE"}, flag.EnvVars)
		require.NoError(t, flag.Action(nil, ""))
		require.NoError(t, flag.Action(nil, "gram-dev-network-ingress"))
		for _, value := range []string{"queue:other", "queue/other", "queue with spaces", strings.Repeat("a", 129)} {
			require.Error(t, flag.Action(nil, value), value)
		}
	}
}

func TestNetworkIngressProviderFlagsRejectInvalidValues(t *testing.T) {
	t.Parallel()
	flags := networkIngressProviderFlags()
	for _, test := range []struct {
		name, valid, invalid string
	}{
		{"network-ingress-operator-namespace", "tailscale", "Invalid Namespace"},
		{"network-ingress-worker-namespace", "gram-dev", "other/namespace"},
		{"network-ingress-backend-namespace", "gram-dev", "other/namespace"},
		{"network-ingress-backend-service", "gram-private", "https://service"},
		{"network-ingress-attestor-ca-secret", "gram-private-ca", "secret/name"},
		{"network-ingress-attestor-image", "registry.example/gram@sha256:" + strings.Repeat("a", 64), "registry.example/gram:latest"},
		{"network-ingress-backend-pod-labels", "app.kubernetes.io/name=gram-server", "app in (server)"},
		{"network-ingress-proxy-tag", "tag:gram-proxy", "gram-proxy"},
		{"network-ingress-service-tag", "tag:gram-service", "tag:"},
		{"network-ingress-class", "tailscale", "nginx"},
		{"network-ingress-token-audience", "gram-netingress", "other"},
		{"network-ingress-kubernetes-api-cidr", "10.0.0.1/32", "10.0.0.1"},
	} {
		flag, ok := requireFlag(t, flags, test.name).(*cli.StringFlag)
		require.True(t, ok, test.name)
		require.Empty(t, flag.Value, test.name)
		require.NoError(t, flag.Action(nil, ""), test.name)
		require.NoError(t, flag.Action(nil, test.valid), test.name)
		require.Error(t, flag.Action(nil, test.invalid), test.name)
	}
	for _, name := range []string{"network-ingress-backend-port", "network-ingress-kubernetes-api-port"} {
		flag, ok := requireFlag(t, flags, name).(*cli.IntFlag)
		require.True(t, ok)
		require.Zero(t, flag.Value)
		require.NoError(t, flag.Action(nil, 0))
		require.NoError(t, flag.Action(nil, 443))
		require.Error(t, flag.Action(nil, -1))
		require.Error(t, flag.Action(nil, 65536))
	}
	cidrs, ok := requireFlag(t, flags, "network-ingress-cluster-cidrs").(*cli.StringSliceFlag)
	require.True(t, ok)
	require.NoError(t, cidrs.Action(nil, nil))
	require.NoError(t, cidrs.Action(nil, []string{"10.0.0.0/8", "fd00::/8"}))
	require.Error(t, cidrs.Action(nil, []string{""}))
	require.Error(t, cidrs.Action(nil, []string{"10.0.0.0"}))
}

func TestNetworkIngressConfigFromCLIDefaultsDisabled(t *testing.T) {
	t.Parallel()
	c := networkIngressTestCLI(t)
	require.NoError(t, c.Set("worker-task-queue", "general-worker"))
	config, err := networkIngressConfigFromCLI(c)
	require.NoError(t, err)
	require.Empty(t, config.ReconcileTaskQueue)
	require.False(t, config.ProviderMutationsEnabled)
	require.False(t, config.MutationReady())
}

func TestNetworkIngressConfigFromCLIReadsProviderSettings(t *testing.T) {
	t.Parallel()
	c := networkIngressTestCLI(t)
	for name, value := range map[string]string{
		networkIngressQueueFlag:               "gram-dev-network-ingress",
		networkIngressMutationFlag:            "true",
		"network-ingress-operator-namespace":  "tailscale",
		"network-ingress-worker-namespace":    "gram-dev",
		"network-ingress-backend-namespace":   "gram-dev",
		"network-ingress-backend-service":     "gram-private",
		"network-ingress-backend-port":        "8443",
		"network-ingress-attestor-image":      "registry.example/gram@sha256:" + strings.Repeat("a", 64),
		"network-ingress-attestor-ca-secret":  "gram-private-ca",
		"network-ingress-backend-pod-labels":  "app.kubernetes.io/name=gram-server",
		"network-ingress-proxy-tag":           "tag:gram-proxy",
		"network-ingress-service-tag":         "tag:gram-service",
		"network-ingress-class":               "tailscale",
		"network-ingress-token-audience":      "gram-netingress",
		"network-ingress-kubernetes-api-cidr": "10.0.0.1/32",
		"network-ingress-kubernetes-api-port": "443",
		"network-ingress-cluster-cidrs":       "10.0.0.0/8,fd00::/8",
	} {
		require.NoError(t, c.Set(name, value), name)
	}
	config, err := networkIngressConfigFromCLI(c)
	require.NoError(t, err)
	require.True(t, config.MutationReady())
	require.Equal(t, "gram-dev-network-ingress", config.ReconcileTaskQueue)
	require.Equal(t, "gram-private", config.BackendService)
	require.Equal(t, int32(8443), config.BackendPort)
	require.Equal(t, "gram-dev", config.Tailscale.BackendNamespace)
	require.Equal(t, map[string]string{"app.kubernetes.io/name": "gram-server"}, config.Tailscale.BackendPodLabels)
	require.Equal(t, int32(443), config.Tailscale.KubernetesAPIPort)
	require.Equal(t, []string{"10.0.0.0/8", "fd00::/8"}, config.Tailscale.ClusterCIDRs)
}

func TestNetworkIngressConfigFromCLIValidatesWithoutFlagActions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, value string }{
		{networkIngressQueueFlag, "invalid/queue"},
		{"network-ingress-attestor-image", "registry.example/gram:latest"},
		{"network-ingress-backend-pod-labels", "app in (server)"},
		{"network-ingress-backend-port", "4294967297"},
		{"network-ingress-kubernetes-api-port", "65536"},
	} {
		c := networkIngressTestCLI(t)
		require.NoError(t, c.Set(test.name, test.value))
		_, err := networkIngressConfigFromCLI(c)
		require.Error(t, err, test.name)
	}
}

func networkIngressTestCLI(t *testing.T) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("network-ingress", flag.ContinueOnError)
	flags := append(networkIngressQueueFlags(), networkIngressProviderFlags()...)
	for _, entry := range flags {
		// Environment is deliberately excluded so defaults do not depend on the host.
		switch entry := entry.(type) {
		case *cli.StringFlag:
			entry.EnvVars = nil
		case *cli.BoolFlag:
			entry.EnvVars = nil
		case *cli.IntFlag:
			entry.EnvVars = nil
		case *cli.StringSliceFlag:
			entry.EnvVars = nil
		}
		require.NoError(t, entry.Apply(set))
	}
	set.String("worker-task-queue", "", "")
	return cli.NewContext(nil, set, nil)
}

func TestNetworkIngressRuntimeDefaultsDisabled(t *testing.T) {
	t.Parallel()
	flag, ok := requireFlag(t, newStartCommand().Flags, "network-ingress-enabled").(*cli.BoolFlag)
	require.True(t, ok)
	require.False(t, flag.Value)
	require.Equal(t, []string{"GRAM_NETWORK_INGRESS_ENABLED"}, flag.EnvVars)
}

func TestOrdinaryWorkerDoesNotOwnNetworkIngressFlags(t *testing.T) {
	t.Parallel()
	command := newWorkerCommand()
	for _, name := range []string{networkIngressQueueFlag, networkIngressMutationFlag, "network-ingress-operator-namespace"} {
		for _, candidate := range command.Flags {
			require.NotEqual(t, name, candidate.Names()[0])
		}
	}
}

func TestNetworkIngressWorkerUsesDedicatedQueue(t *testing.T) {
	t.Parallel()
	command := newNetworkIngressWorkerCommand()
	for _, candidate := range command.Flags {
		require.NotEqual(t, "temporal-task-queue", candidate.Names()[0])
	}
	sharedQueue, ok := requireFlag(t, command.Flags, "shared-worker-task-queue").(*cli.StringFlag)
	require.True(t, ok)
	require.True(t, sharedQueue.Hidden)
	require.Equal(t, []string{"TEMPORAL_TASK_QUEUE"}, sharedQueue.EnvVars)
	queue, ok := requireFlag(t, command.Flags, networkIngressQueueFlag).(*cli.StringFlag)
	require.True(t, ok)
	require.Empty(t, queue.Value)
	require.False(t, queue.Required, "config-file loading happens before the command validates an empty queue")
}

func TestNetworkIngressWorkerQueueRejectsSharedMainQueue(t *testing.T) {
	t.Parallel()
	require.Error(t, validateNetworkIngressWorkerQueue("", "main"))
	require.Error(t, validateNetworkIngressWorkerQueue("main", "main"))
	require.Error(t, validateNetworkIngressWorkerQueue("ordinary", "ordinary"))
	require.NoError(t, validateNetworkIngressWorkerQueue("gram-dev-network-ingress", "main"))
}

func TestAppRegistersNetworkIngressProcesses(t *testing.T) {
	t.Parallel()
	commands := map[string]bool{}
	for _, command := range newApp().Commands {
		commands[command.Name] = true
	}
	require.True(t, commands["network-ingress-server"])
	require.True(t, commands["network-ingress-worker"])
}

func TestNetworkIngressServerUsesPrivateListenerFlags(t *testing.T) {
	t.Parallel()
	command := newNetworkIngressServerCommand()
	require.Equal(t, "network-ingress-server", command.Name)
	for _, name := range []string{"netingress-address", "netingress-tls-cert-file", "netingress-tls-key-file"} {
		flag, ok := requireFlag(t, command.Flags, name).(*cli.StringFlag)
		require.True(t, ok, name)
		require.NotEmpty(t, flag.EnvVars, name)
	}
}

func TestValidatePrivateServerConfig(t *testing.T) {
	t.Parallel()
	require.Error(t, validatePrivateServerConfig(false, ":8443", "cert", "key", false))
	require.Error(t, validatePrivateServerConfig(true, "", "cert", "key", false))
	require.Error(t, validatePrivateServerConfig(true, ":8443", "", "key", false))
	require.Error(t, validatePrivateServerConfig(true, ":8443", "cert", "", false))
	require.Error(t, validatePrivateServerConfig(true, ":8443", "cert", "key", true))
	require.NoError(t, validatePrivateServerConfig(true, ":8443", "cert", "key", false))
}

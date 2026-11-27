package gram

import (
	"flag"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/networkingress"
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

func TestNetworkIngressProviderFlagsRejectInvalidTag(t *testing.T) {
	t.Parallel()
	flag, ok := requireFlag(t, networkIngressProviderFlags(), "network-ingress-gateway-tag").(*cli.StringFlag)
	require.True(t, ok)
	require.Equal(t, networkingress.DefaultGatewayTag, flag.Value)
	require.NoError(t, flag.Action(nil, ""))
	require.NoError(t, flag.Action(nil, "tag:speakeasy-dev"))
	require.Error(t, flag.Action(nil, "speakeasy"))
	require.Error(t, flag.Action(nil, "tag:"))
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
		networkIngressQueueFlag:         "gram-dev-network-ingress",
		networkIngressMutationFlag:      "true",
		"network-ingress-gateway-tag":   "tag:speakeasy-dev",
		"network-ingress-gateway-slots": "2",
	} {
		require.NoError(t, c.Set(name, value), name)
	}
	config, err := networkIngressConfigFromCLI(c)
	require.NoError(t, err)
	require.True(t, config.MutationReady())
	require.Equal(t, "gram-dev-network-ingress", config.ReconcileTaskQueue)
	require.Equal(t, "tag:speakeasy-dev", config.Gateway.Tag)
	require.Equal(t, int32(2), config.Gateway.Slots)
}

func TestNetworkIngressConfigFromCLIValidatesWithoutFlagActions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, value string }{
		{networkIngressQueueFlag, "invalid/queue"},
		{"network-ingress-gateway-slots", "17"},
		{"network-ingress-tailscale-api-url", "http://api.tailscale.com"},
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
	for _, name := range []string{networkIngressQueueFlag, networkIngressMutationFlag, "network-ingress-gateway-tag"} {
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

func TestTailnetGatewayRegistersOnlyTheEncryptionSettingsItReads(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	for _, flag := range newTailnetGatewayCommand().Flags {
		names[flag.Names()[0]] = true
	}
	require.True(t, names[orgEncryptionCredentialIDFlag], "the gateway decrypts with the organization's key")
	require.False(t, names[orgEncryptionSwitchGracePeriodFlag], "key switching is the provisioner's, not the gateway's")
	require.False(t, names[orgEncryptionManagedKeyRetention])
}

func TestLocalDevelopmentNamerRequiresADevelopmentID(t *testing.T) {
	t.Parallel()

	_, err := localDevelopmentNamer("local", "", "")
	require.ErrorContains(t, err, "GRAM_NETWORK_INGRESS_DEV_ID", "a local stack must not fall back to production names")

	namer, err := localDevelopmentNamer("local", "ab12", "")
	require.NoError(t, err)
	require.IsType(t, networkingress.LocalNamer{}, namer)

	namer, err = localDevelopmentNamer("prod", "", "")
	require.NoError(t, err)
	require.IsType(t, networkingress.ProductionNamer{}, namer)

	_, err = localDevelopmentNamer("prod", "ab12", "")
	require.ErrorIs(t, err, networkingress.ErrDevIDOutsideLocal)

	namer, err = localDevelopmentNamer("dev", "", "dev")
	require.NoError(t, err)
	require.IsType(t, networkingress.QualifiedNamer{}, namer)
}

func TestGatewayNameQualifierReachesReconcilerAndGateway(t *testing.T) {
	t.Parallel()

	commands := map[string][]cli.Flag{
		"network-ingress-worker": networkIngressProviderFlags(),
		"tailnet-gateway":        newTailnetGatewayCommand().Flags,
	}
	for name, flags := range commands {
		var envVars []string
		for _, flag := range flags {
			if flag.Names()[0] == networkIngressGatewayNameQualifierFlag {
				stringFlag, ok := flag.(*cli.StringFlag)
				require.True(t, ok)
				envVars = stringFlag.EnvVars
			}
		}
		require.Equal(t, []string{"GRAM_NETWORK_INGRESS_GATEWAY_NAME_QUALIFIER"}, envVars, "%s reads the qualifier", name)
	}
}

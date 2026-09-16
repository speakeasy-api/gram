package gram

import (
	"context"
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestServerCommandsOwnSeparateListenerFlags(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		command *cli.Command
		absent  []string
	}{
		{newStartCommand(), []string{"netingress-address", "netingress-tls-cert-file", "netingress-tls-key-file"}},
		{newNetworkIngressServerCommand(), []string{"address", "ssl-key-file", "ssl-cert-file"}},
	} {
		seen := map[string]bool{}
		for _, f := range test.command.Flags {
			for _, name := range f.Names() {
				require.NotContains(t, test.absent, name, test.command.Name)
				require.False(t, seen[name], "duplicate flag %s on %s", name, test.command.Name)
				seen[name] = true
			}
		}
	}
	for _, name := range []string{"address", "ssl-key-file", "ssl-cert-file"} {
		requireFlag(t, newStartCommand().Flags, name)
	}
	requireFlag(t, newStartCommand().Flags, "network-ingress-enabled")
	requireFlag(t, newStartCommand().Flags, networkIngressQueueFlag)
	for _, name := range []string{"gcp-project-id", "local-kms-signing-algorithm", "pubsub-emulator-host"} {
		requireFlag(t, newNetworkIngressServerCommand().Flags, name)
	}
}

func TestPrivateIngressActionRejectsDisabledRuntimeBeforeDependencies(t *testing.T) {
	t.Parallel()
	set := flag.NewFlagSet("private-ingress", flag.ContinueOnError)
	set.Bool("network-ingress-enabled", false, "")
	c := cli.NewContext(nil, set, nil)
	err := newNetworkIngressServerCommand().Action(c)
	require.EqualError(t, err, "private network ingress runtime is disabled")
}

func TestPrivateIngressRuntimeClosesInReverseOrderAfterCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var closed []int
	runtime := &privateIngressRuntime{}
	for i := range 3 {
		runtime.cleanup = append(runtime.cleanup, func(ctx context.Context) error {
			require.NoError(t, ctx.Err())
			closed = append(closed, i)
			return nil
		})
	}
	runtime.Close(ctx)
	require.Equal(t, []int{2, 1, 0}, closed)
}

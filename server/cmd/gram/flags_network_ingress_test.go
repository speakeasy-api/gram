package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestNetworkIngressRuntimeDefaultsDisabled(t *testing.T) {
	t.Parallel()
	flag, ok := requireFlag(t, newStartCommand().Flags, "network-ingress-enabled").(*cli.BoolFlag)
	require.True(t, ok)
	require.False(t, flag.Value)
	require.Equal(t, []string{"GRAM_NETWORK_INGRESS_ENABLED"}, flag.EnvVars)
}

package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestPluginPublicationFlagsDefaultOff(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		flag cli.Flag
		name string
		env  string
	}{
		{pluginPublicationEmitFlag(), pluginPublicationEmitFlagName, "GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED"},
		{pluginPublicationConsumeFlag(), pluginPublicationConsumeFlagName, "GRAM_PLUGIN_PUBLICATION_CONSUME_ENABLED"},
	} {
		actual, ok := tc.flag.(*cli.BoolFlag)
		require.True(t, ok)
		require.Equal(t, tc.name, actual.Name)
		require.False(t, actual.Value)
		require.Equal(t, []string{tc.env}, actual.EnvVars)
	}
}

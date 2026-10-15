package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestWorkerPublicationFlag(t *testing.T) {
	for _, newCommand := range []func() *cli.Command{newStartCommand, newWorkerCommand, newStreamsCommand} {
		t.Run(newCommand().Name, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				env  string
				args []string
				want bool
			}{
				{name: "default disabled"},
				{name: "environment enabled", env: "true", want: true},
				{name: "environment disabled", env: "false"},
				{name: "CLI enabled", args: []string{"--plugin-publication-emit-enabled"}, want: true},
				{name: "CLI overrides environment", env: "true", args: []string{"--plugin-publication-emit-enabled=false"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Setenv("GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED", tc.env)
					command := newCommand()
					registered := requireFlag(t, command.Flags, pluginPublicationEmitFlagName)
					publicationFlag, ok := registered.(*cli.BoolFlag)
					require.True(t, ok)
					require.False(t, publicationFlag.Value)
					require.Equal(t, []string{"GRAM_PLUGIN_PUBLICATION_EMIT_ENABLED"}, publicationFlag.EnvVars)
					set := flag.NewFlagSet(command.Name, flag.ContinueOnError)
					require.NoError(t, publicationFlag.Apply(set))
					require.NoError(t, set.Parse(tc.args))
					ctx := cli.NewContext(nil, set, nil)
					require.Equal(t, tc.want, ctx.Bool(pluginPublicationEmitFlagName))
				})
			}
		})
	}
}

package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestRenderPlatformMCPServerURL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "canonical default", args: nil, want: "https://ai.speakeasy.com"},
		{name: "custom deployment", args: []string{"--server-url=https://example.com"}, want: "https://example.com"},
		{name: "local override", args: []string{"--server-url=http://localhost:8080"}, want: "http://localhost:8080"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := newRenderPlatformMCPCommand()
			flags := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
			for _, f := range cmd.Flags {
				require.NoError(t, f.Apply(flags))
			}
			require.NoError(t, flags.Parse(tt.args))
			ctx := cli.NewContext(cli.NewApp(), flags, nil)
			require.Equal(t, tt.want, ctx.String("server-url"))
		})
	}
}

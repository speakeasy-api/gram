//go:build codemodeintegration

package gram

import (
	"bytes"
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCodeRuntimeSmokePassesAgainstRealRunner(t *testing.T) {
	t.Parallel()
	endpoint, token := testenv.LaunchCodeRunner(t)

	command := newCodeRuntimeSmokeCommand()
	set := flag.NewFlagSet(command.Name, flag.ContinueOnError)
	for _, f := range command.Flags {
		require.NoError(t, f.Apply(set))
	}
	require.NoError(t, set.Parse([]string{
		"--environment", "local",
		"--code-runtime-provider", "local",
		"--code-runtime-url", endpoint,
		"--code-runtime-token", token,
		"--runs", "3",
	}))
	c := cli.NewContext(cli.NewApp(), set, nil)
	c.Context = PushLogger(t.Context(), testenv.NewLogger(t))

	var out bytes.Buffer
	require.NoError(t, runCodeRuntimeSmoke(c, &out), out.String())
	require.Contains(t, out.String(), "all checks passed")
	require.Contains(t, out.String(), "4 at once")
}

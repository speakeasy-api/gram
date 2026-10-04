package gram

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// TestMCPServerFlags pins the contract the MCP tier has with its deployment:
// it listens like `gram start` and accepts the same runtime environment,
// including the Temporal flags it never reads, so the same env can be
// mounted on both tiers without the chart withholding values.
func TestMCPServerFlags(t *testing.T) {
	t.Parallel()

	names := flagNames(mcpServerFlags())
	for _, want := range []string{"address", "ssl-key-file", "ssl-cert-file", "temporal-address", "temporal-namespace", "temporal-task-queue", "database-url", "redis-cache-addr", "server-url"} {
		require.Containsf(t, names, want, "mcp command must accept the %q flag", want)
	}
	require.NotContains(t, names, "loops-api-key", "mcp command must not require email delivery configuration")
	require.NotContains(t, names, "workos-webhook-secret", "mcp command must not require webhook configuration")
}

func TestMCPCommandRegistered(t *testing.T) {
	t.Parallel()

	cmd := newMCPCommand()
	require.Equal(t, "mcp", cmd.Name)
	require.NotNil(t, cmd.Action)
	require.NotNil(t, cmd.Before)
}

func flagNames(flags []cli.Flag) []string {
	names := make([]string, 0, len(flags))
	for _, f := range flags {
		names = slices.Concat(names, f.Names())
	}
	return names
}

package gram

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/mcp"
)

// One keepalive re-check must finish inside the probe drain, with room for the drain's own bookkeeping.
func TestRemoteSessionRecheckBudgetFitsProbeDrain(t *testing.T) {
	t.Parallel()
	require.LessOrEqual(t, mcp.RemoteSessionRecheckProbeBudgetCap, probeDrainTimeout-2*time.Second)
}

func TestRemoteSessionRecheckIntervalDefaultsOn(t *testing.T) {
	t.Parallel()
	flag, ok := requireFlag(t, newStartCommand().Flags, "remote-session-recheck-interval").(*cli.DurationFlag)
	require.True(t, ok)
	require.Equal(t, mcp.DefaultRemoteSessionRecheckInterval, flag.Value)
	require.Contains(t, flag.Usage, "disables")
}

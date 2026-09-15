package gram

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/mcp"
)

// Mid-batch cancellation drains active probes, then releases unstarted claims.
// Both steps must fit inside the server drain, with bookkeeping headroom.
func TestRemoteSessionRecheckBudgetFitsProbeDrain(t *testing.T) {
	t.Parallel()
	require.LessOrEqual(t, mcp.RemoteSessionRecheckProbeBudgetCap+mcp.RemoteSessionRecheckLeaseReleaseBudget, probeDrainTimeout-time.Second)
}

func TestRemoteSessionRecheckIntervalDefaultsOn(t *testing.T) {
	t.Parallel()
	flag, ok := requireFlag(t, newStartCommand().Flags, "remote-session-recheck-interval").(*cli.DurationFlag)
	require.True(t, ok)
	require.Equal(t, mcp.DefaultRemoteSessionRecheckInterval, flag.Value)
	require.Contains(t, flag.Usage, "disables")
}

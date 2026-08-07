package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The refresh timing ladder is load-bearing rather than cosmetic, and nothing
// at runtime notices when it drifts: the symptom of an inverted ordering is a
// user being told to reconnect a working session, only under a slow upstream.
func TestRefreshTimingInvariant(t *testing.T) {
	t.Parallel()

	require.Less(t, refreshUpstreamTimeout, refreshLockTTL,
		"the upstream POST must finish or fail inside the lease, or the holder keeps using a refresh token after its lock is gone")

	require.LessOrEqual(t, refreshLockTTL, refreshWaitBudget,
		"a waiter must not give up while the lease could still be held, or it presents the refresh token the holder is still using")

	require.Less(t, refreshWaitPoll, refreshWaitBudget,
		"the wait must poll at least once before giving up")
}

package orgencryption

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunLeaseOutlastsALiveRun(t *testing.T) {
	t.Parallel()

	require.Greater(t, RunLease, MaxRunDuration+recordTimeout, "a live run, its deadline and the write that ends it, never outlasts its claim")
}

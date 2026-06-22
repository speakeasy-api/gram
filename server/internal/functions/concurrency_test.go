package functions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConcurrencyLimits_DerivesFromSlots(t *testing.T) {
	t.Parallel()
	soft, hard := concurrencyLimits(8)
	require.Equal(t, 16, hard, "hard limit is 2*N so the proxy admits a bounded queue beyond execution capacity")
	require.Equal(t, 5, soft, "soft limit is round(0.65*N), below N for early autostart")
}

func TestConcurrencyLimits_LadderSoftBelowNBelowHard(t *testing.T) {
	t.Parallel()
	// The intended ordering is soft < N < hard: the runner queue (and its 429)
	// forms between N and hard, while autostart fires at soft, below N.
	for slots := 2; slots <= 64; slots++ {
		soft, hard := concurrencyLimits(slots)
		require.Equal(t, 2*slots, hard, "hard is 2*N, slots=%d", slots)
		require.Less(t, soft, slots, "soft stays below N so autostart triggers with execution headroom, slots=%d", slots)
		require.Less(t, slots, hard, "N sits below hard so the runner holds the overflow before the proxy sheds, slots=%d", slots)
		require.GreaterOrEqual(t, soft, 1)
	}
}

func TestConcurrencyLimits_Floor(t *testing.T) {
	t.Parallel()
	// executionSlots never returns < 4, so slots <= 1 is unreachable in practice
	// but kept correct for the standalone helper: N floors at 1, giving hard 2 and
	// soft 1.
	for _, slots := range []int{0, 1} {
		soft, hard := concurrencyLimits(slots)
		require.Equal(t, 2, hard, "hard floors at 2 (2*1), slots=%d", slots)
		require.Equal(t, 1, soft, "soft floors at 1, slots=%d", slots)
	}
}

func TestExecutionSlots_ScalesWithMemoryUpToCap(t *testing.T) {
	t.Parallel()
	// node: memPerSlot=128, cap=24, floor=4.
	require.Equal(t, 4, executionSlots(RuntimeNodeJS22, 256), "raw 2 floors at 4")
	require.Equal(t, 4, executionSlots(RuntimeNodeJS22, 512), "exactly the floor")
	require.Equal(t, 8, executionSlots(RuntimeNodeJS22, 1024), "default tier")
	require.Equal(t, 11, executionSlots(RuntimeNodeJS22, 1500), "arbitrary in-between memory")
	require.Equal(t, 16, executionSlots(RuntimeNodeJS22, 2048))
	require.Equal(t, 24, executionSlots(RuntimeNodeJS22, 4096), "raw 32 capped at the CPU ceiling")
}

func TestExecutionSlots_NodeRuntimesMatch(t *testing.T) {
	t.Parallel()
	for _, mem := range []int{512, 1024, 2048, 4096} {
		require.Equal(t, executionSlots(RuntimeNodeJS22, mem), executionSlots(RuntimeNodeJS24, mem), "node 22 and 24 size identically, mem=%d", mem)
		require.Equal(t, executionSlots(RuntimeNodeJS22, mem), executionSlots(RuntimePython312, mem), "python sizes like node, mem=%d", mem)
	}
}

func TestExecutionSlots_UnknownRuntimeFallback(t *testing.T) {
	t.Parallel()
	// fallback: memPerSlot=192, cap=8, floor=4.
	require.Equal(t, 5, executionSlots(Runtime("unknown"), 1024), "raw 5 from the conservative per-slot budget")
	require.Equal(t, 8, executionSlots(Runtime("unknown"), 4096), "raw 21 capped at the conservative ceiling")
	require.Equal(t, 4, executionSlots(Runtime("unknown"), 256), "floors at 4")
}

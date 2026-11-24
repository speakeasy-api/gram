package intentpolicy_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

func TestWindow(t *testing.T) {
	t.Parallel()
	actions := make([]intentpolicy.Action, intentpolicy.MaxActions+10)
	for i := range actions {
		actions[i].Seq = int64(i)
	}
	seqs := func(w []intentpolicy.Action) []int64 {
		out := make([]int64, len(w))
		for i, a := range w {
			out[i] = a.Seq
		}
		return out
	}

	require.Len(t, intentpolicy.Window(actions, 0, 0), intentpolicy.MaxActions)
	require.Equal(t, []int64{5, 6, 7}, seqs(intentpolicy.Window(actions, 5, 3)))
	require.Len(t, intentpolicy.Window(actions, intentpolicy.MaxActions-2, 8), 2, "never past the cap")
	require.Empty(t, intentpolicy.Window(actions, intentpolicy.MaxActions+5, 8))
	require.Equal(t, []int64{0, 1}, seqs(intentpolicy.Window(actions[:2], -1, 0)))
}

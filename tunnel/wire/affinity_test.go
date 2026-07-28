package wire

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRendezvousOrderDeterministic(t *testing.T) {
	t.Parallel()

	candidates := []string{"gateway-c", "gateway-a", "gateway-b"}

	require.Equal(t, RendezvousOrder("stable-client", candidates), RendezvousOrder("stable-client", candidates))
	require.ElementsMatch(t, candidates, RendezvousOrder("stable-client", candidates))
}

func TestRendezvousOrderEmptyKey(t *testing.T) {
	t.Parallel()

	require.Nil(t, RendezvousOrder("", []string{"gateway-a"}))
}

func TestRendezvousPickHonorsExclude(t *testing.T) {
	t.Parallel()

	candidates := []string{"gateway-a", "gateway-b", "gateway-c"}
	first, ok := RendezvousPick("stable-client", candidates, nil)
	require.True(t, ok)

	picked, ok := RendezvousPick("stable-client", candidates, map[string]struct{}{first: {}})
	require.True(t, ok)
	require.NotEqual(t, first, picked)
	require.Contains(t, candidates, picked)
}

func TestOrderRendezvousCandidatesTieBreaksByValue(t *testing.T) {
	t.Parallel()

	ordered := orderRendezvousCandidates([]rendezvousCandidate{
		{value: "gateway-b", score: 10},
		{value: "gateway-c", score: 20},
		{value: "gateway-a", score: 10},
	})

	require.Equal(t, []string{"gateway-c", "gateway-a", "gateway-b"}, ordered)
}

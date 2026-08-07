package activities

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHighestCrossedOpenRouterCreditsThreshold_Ladder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		used  float64
		limit int64
		want  int
	}{
		{used: 0, limit: 1000, want: 0},
		{used: 499, limit: 1000, want: 0},
		{used: 500, limit: 1000, want: 50},
		{used: 749.9, limit: 1000, want: 50},
		{used: 750, limit: 1000, want: 75},
		{used: 899, limit: 1000, want: 75},
		{used: 900, limit: 1000, want: 90},
		{used: 999.9, limit: 1000, want: 90},
		{used: 1000, limit: 1000, want: 100},
		// No beyond-100 escalation for credits: exhausted is exhausted.
		{used: 1500, limit: 1000, want: 100},
		// A zero or negative limit yields no signal rather than dividing by zero.
		{used: 500, limit: 0, want: 0},
		{used: 500, limit: -1, want: 0},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, highestCrossedOpenRouterCreditsThreshold(tc.used, tc.limit),
			"used=%v limit=%d", tc.used, tc.limit)
	}
}

func TestHighestCrossedAlertThreshold_EscalationModes(t *testing.T) {
	t.Parallel()

	// The shared ladder differs between its consumers only past 100%.
	require.Equal(t, int64(100), highestCrossedAlertThreshold(160, false),
		"without escalation the ladder tops out at 100")
	require.Equal(t, int64(150), highestCrossedAlertThreshold(160, true),
		"with escalation each further 50%% adds a rung")
}

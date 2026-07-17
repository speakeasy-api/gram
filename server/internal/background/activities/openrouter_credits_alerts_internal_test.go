package activities

import (
	"testing"
	"time"

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

func TestStartOfNextMonthUTC(t *testing.T) {
	t.Parallel()

	// Mid-month rolls to the first of the next month.
	require.Equal(t,
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		startOfNextMonthUTC(time.Date(2026, time.July, 17, 18, 30, 0, 0, time.UTC)),
	)

	// December rolls into the next year.
	require.Equal(t,
		time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		startOfNextMonthUTC(time.Date(2026, time.December, 31, 23, 59, 59, 0, time.UTC)),
	)
}

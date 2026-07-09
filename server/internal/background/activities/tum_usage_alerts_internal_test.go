package activities

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHighestCrossedTumThreshold_Ladder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		usage int64
		limit int64
		want  int64
	}{
		{usage: 0, limit: 1000, want: 0},
		{usage: 499, limit: 1000, want: 0},
		{usage: 500, limit: 1000, want: 50},
		{usage: 749, limit: 1000, want: 50},
		{usage: 750, limit: 1000, want: 75},
		{usage: 899, limit: 1000, want: 75},
		{usage: 900, limit: 1000, want: 90},
		{usage: 999, limit: 1000, want: 90},
		{usage: 1000, limit: 1000, want: 100},
		{usage: 1499, limit: 1000, want: 100},
		{usage: 1500, limit: 1000, want: 150},
		{usage: 2000, limit: 1000, want: 200},
		{usage: 3499, limit: 1000, want: 300},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, highestCrossedTumThreshold(tc.usage, tc.limit),
			"usage=%d limit=%d", tc.usage, tc.limit)
	}
}

func TestFormatTokenCount_GroupsThousands(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0", formatTokenCount(0))
	require.Equal(t, "999", formatTokenCount(999))
	require.Equal(t, "1,000", formatTokenCount(1000))
	require.Equal(t, "45,000,000", formatTokenCount(45_000_000))
	require.Equal(t, "1,234,567,890", formatTokenCount(1_234_567_890))
}

package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeeklyUsageSummary_Key(t *testing.T) {
	t.Parallel()

	require.Equal(t, TemplateKeyWeeklyUsageSummary, WeeklyUsageSummary{}.Key())
}

func TestWeeklyUsageSummary_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, WeeklyUsageSummary{}.AddToAudience(),
		"usage digests should not add recipients to the Loops audience")
}

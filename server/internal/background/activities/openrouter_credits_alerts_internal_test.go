package activities

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func OpenRouterCreditsAlertGenerationKeyForTest(orgID string, keyType openrouter.KeyType) string {
	return openRouterCreditsAlertGenerationKey(orgID, keyType)
}

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

	require.Equal(t, int64(100), highestCrossedAlertThreshold(160, false),
		"without escalation the ladder tops out at 100")
	require.Equal(t, int64(150), highestCrossedAlertThreshold(160, true),
		"with escalation each further 50%% adds a rung")
}

func TestOpenRouterCreditsAlertCycleStaysStableThroughRolloverGrace(t *testing.T) {
	t.Parallel()

	require.Equal(t, "2026-07", openRouterCreditsAlertCycle(time.Date(2026, time.July, 31, 23, 30, 0, 0, time.UTC)))
	require.Equal(t, "2026-07", openRouterCreditsAlertCycle(time.Date(2026, time.August, 1, 0, 30, 0, 0, time.UTC)))
	require.Equal(t, "2026-08", openRouterCreditsAlertCycle(time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)))
}

func TestOpenRouterCreditsAlertKeyPreservesLegacyReservationsUntilCapChange(t *testing.T) {
	t.Parallel()

	require.Equal(
		t,
		"openrouter-credits-alert:org_placeholder:chat:90",
		openRouterCreditsAlertKey("org_placeholder", "chat", 90, ""),
	)
	require.Equal(
		t,
		"openrouter-credits-alert:org_placeholder:chat:90:operation_placeholder",
		openRouterCreditsAlertKey("org_placeholder", "chat", 90, "operation_placeholder"),
	)
}

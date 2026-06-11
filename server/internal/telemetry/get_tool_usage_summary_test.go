package telemetry_test

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/stretchr/testify/require"
)

func TestGetToolUsageSummary_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	result, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int64(0), result.Totals.EventCount)
	require.Equal(t, int64(0), result.Totals.SuccessCount)
	require.Equal(t, int64(0), result.Totals.FailureCount)
	require.Equal(t, float64(0), result.Totals.FailureRate)
	require.Equal(t, int64(0), result.Totals.UniqueTools)
	require.Equal(t, int64(0), result.Totals.UniqueUsers)
	require.Equal(t, int64(0), result.Totals.UniqueTargets)
	require.Empty(t, result.Targets)
	require.Empty(t, result.Users)
	require.Empty(t, result.TargetTimeSeries)
	require.Empty(t, result.UserTimeSeries)
	require.Empty(t, result.UsersByTarget)
	require.Empty(t, result.TargetToolBreakdown)
}

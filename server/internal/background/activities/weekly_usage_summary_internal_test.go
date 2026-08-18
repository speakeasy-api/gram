package activities

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usage"
)

func TestUsageChangePercent_Increase(t *testing.T) {
	t.Parallel()

	require.Equal(t, "+19%", usageChangePercent(1190, 1000))
}

func TestUsageChangePercent_Decrease(t *testing.T) {
	t.Parallel()

	require.Equal(t, "-92%", usageChangePercent(80, 1000))
}

func TestUsageChangePercent_Flat(t *testing.T) {
	t.Parallel()

	require.Equal(t, "+0%", usageChangePercent(1000, 1000))
}

func TestUsageChangePercent_NoPreviousUsage(t *testing.T) {
	t.Parallel()

	require.Equal(t, "New", usageChangePercent(500, 0))
	require.Equal(t, "0%", usageChangePercent(0, 0))
}

func TestUsageChangePercent_DroppedToZero(t *testing.T) {
	t.Parallel()

	require.Equal(t, "-100%", usageChangePercent(0, 1000))
}

func TestDaysUntil_RoundsPartialDaysUp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	require.Equal(t, 8, daysUntil(now, end))
}

func TestDaysUntil_PastEndIsZero(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	require.Equal(t, 0, daysUntil(now, now))
	require.Equal(t, 0, daysUntil(now.Add(time.Hour), now))
}

func TestFormatDaysRemaining_Pluralizes(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0 days", formatDaysRemaining(0))
	require.Equal(t, "1 day", formatDaysRemaining(1))
	require.Equal(t, "8 days", formatDaysRemaining(8))
}

func TestElapsedPercent_MidCycle(t *testing.T) {
	t.Parallel()

	cycle := usage.BillingCyclePeriod{
		Start: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
	}
	// 22.5 of 31 days.
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	require.Equal(t, 73, elapsedPercent(cycle, now))
}

func TestElapsedPercent_Clamped(t *testing.T) {
	t.Parallel()

	cycle := usage.BillingCyclePeriod{
		Start: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
	}
	require.Equal(t, 0, elapsedPercent(cycle, cycle.Start.Add(-time.Hour)))
	require.Equal(t, 100, elapsedPercent(cycle, cycle.End.Add(time.Hour)))
}

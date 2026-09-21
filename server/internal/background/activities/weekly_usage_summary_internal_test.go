package activities

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageChangePercent_Increase(t *testing.T) {
	t.Parallel()

	require.Equal(t, "+19%", usageChangePercent(big.NewInt(1190), big.NewInt(1000)))
}

func TestUsageChangePercent_Decrease(t *testing.T) {
	t.Parallel()

	require.Equal(t, "-92%", usageChangePercent(big.NewInt(80), big.NewInt(1000)))
}

func TestUsageChangePercent_UsesExactLargeQuantities(t *testing.T) {
	t.Parallel()

	previous, ok := new(big.Int).SetString("900719925474099300000000000", 10)
	require.True(t, ok)
	current := new(big.Int).Add(previous, new(big.Int).Quo(previous, big.NewInt(2)))
	require.Equal(t, "+50%", usageChangePercent(current, previous))
}

func TestUsageChangePercent_NoPreviousUsage(t *testing.T) {
	t.Parallel()

	require.Equal(t, "New", usageChangePercent(big.NewInt(500), new(big.Int)))
	require.Equal(t, "0%", usageChangePercent(new(big.Int), new(big.Int)))
}

func TestUsageChangePercent_DroppedToZero(t *testing.T) {
	t.Parallel()

	require.Equal(t, "-100%", usageChangePercent(new(big.Int), big.NewInt(1000)))
}

func TestFormatUSD_PreservesPositiveSubCentSpend(t *testing.T) {
	t.Parallel()

	require.Equal(t, "<$0.01", formatUSD(big.NewRat(1, 1000)))
	require.Equal(t, "$0.00", formatUSD(new(big.Rat)))
	require.Equal(t, "$12.35", formatUSD(big.NewRat(12345, 1000)))
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

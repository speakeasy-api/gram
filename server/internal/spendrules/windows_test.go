package spendrules_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/spendrules"
)

func TestWindowBoundsDaily(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 4, 15, 30, 12, 0, time.UTC)
	start, end, err := spendrules.WindowBounds(spendrules.WindowDaily, now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), start)
	require.Equal(t, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), end)
}

func TestWindowBoundsWeeklyStartsMonday(t *testing.T) {
	t.Parallel()

	t.Run("saturday", func(t *testing.T) {
		t.Parallel()

		// 2026-07-04 is a Saturday; the containing week starts Monday 2026-06-29.
		now := time.Date(2026, 7, 4, 15, 30, 0, 0, time.UTC)
		start, end, err := spendrules.WindowBounds(spendrules.WindowWeekly, now)
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC), start)
		require.Equal(t, time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), end)
	})

	t.Run("monday", func(t *testing.T) {
		t.Parallel()

		// A Monday belongs to the week it starts.
		monday := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
		start, end, err := spendrules.WindowBounds(spendrules.WindowWeekly, monday)
		require.NoError(t, err)
		require.Equal(t, monday, start)
		require.Equal(t, monday.AddDate(0, 0, 7), end)
	})
}

func TestWindowBoundsMonthly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	start, end, err := spendrules.WindowBounds(spendrules.WindowMonthly, now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), start)
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), end)
}

func TestWindowBoundsNormalizesToUTC(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// 23:00 EDT on Jul 3 is 03:00 UTC on Jul 4 — the UTC day wins.
	now := time.Date(2026, 7, 3, 23, 0, 0, 0, loc)
	start, _, err := spendrules.WindowBounds(spendrules.WindowDaily, now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), start)
}

func TestWindowBoundsRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	_, _, err := spendrules.WindowBounds("fortnightly", time.Now())
	require.Error(t, err)
}

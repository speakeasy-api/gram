package memorydigest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/memorydigest"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestPeriodStart(t *testing.T) {
	t.Parallel()

	// Thursday 2026-10-29, 23:30 in UTC-5 is Friday 04:30 UTC.
	ts := time.Date(2026, 10, 29, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600))

	require.Equal(t, date(2026, 10, 30), memorydigest.LevelDay.PeriodStart(ts))
	require.Equal(t, date(2026, 10, 26), memorydigest.LevelWeek.PeriodStart(ts))
	require.Equal(t, date(2026, 10, 1), memorydigest.LevelMonth.PeriodStart(ts))

	// A Sunday belongs to the week that started the Monday before.
	require.Equal(t, date(2026, 10, 26), memorydigest.LevelWeek.PeriodStart(date(2026, 11, 1)))
	// A Monday starts its own week.
	require.Equal(t, date(2026, 11, 2), memorydigest.LevelWeek.PeriodStart(date(2026, 11, 2)))
}

func TestReadyAt(t *testing.T) {
	t.Parallel()

	require.Equal(t, date(2026, 10, 30), memorydigest.LevelDay.ReadyAt(date(2026, 10, 29)))
	require.Equal(t, date(2026, 11, 2), memorydigest.LevelWeek.ReadyAt(date(2026, 10, 26)))
	// October 2026's last week starts Monday 26 Oct and closes Monday 2 Nov.
	require.Equal(t, date(2026, 11, 2), memorydigest.LevelMonth.ReadyAt(date(2026, 10, 1)))
	// June 2026 ends on a Tuesday; its last week starts 29 June and closes 6 July.
	require.Equal(t, date(2026, 7, 6), memorydigest.LevelMonth.ReadyAt(date(2026, 6, 1)))
}

func TestChildStarts(t *testing.T) {
	t.Parallel()

	require.Empty(t, memorydigest.LevelDay.ChildStarts(date(2026, 10, 29)))

	week := memorydigest.LevelWeek.ChildStarts(date(2026, 10, 26))
	require.Len(t, week, 7)
	require.Equal(t, date(2026, 10, 26), week[0])
	require.Equal(t, date(2026, 11, 1), week[6])

	// Weeks of October 2026 are those whose Monday falls in October.
	month := memorydigest.LevelMonth.ChildStarts(date(2026, 10, 1))
	require.Equal(t, []time.Time{
		date(2026, 10, 5), date(2026, 10, 12), date(2026, 10, 19), date(2026, 10, 26),
	}, month)
}

func TestEveryWeekHasOneParentMonth(t *testing.T) {
	t.Parallel()

	seen := map[time.Time]int{}
	for m := date(2026, 1, 1); m.Before(date(2027, 1, 1)); m = m.AddDate(0, 1, 0) {
		for _, w := range memorydigest.LevelMonth.ChildStarts(m) {
			seen[w]++
			parent, ok := memorydigest.LevelWeek.ParentStart(w)
			require.True(t, ok)
			require.Equal(t, m, parent)
		}
	}
	for w := memorydigest.LevelWeek.PeriodStart(date(2026, 1, 1)); w.Before(date(2027, 1, 1)); w = w.AddDate(0, 0, 7) {
		if w.Year() == 2026 {
			require.Equal(t, 1, seen[w], "week %s", w)
		}
	}
}

func TestReadyPeriods(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)

	days := memorydigest.LevelDay.ReadyPeriods(date(2026, 10, 30), now)
	require.Equal(t, []time.Time{date(2026, 10, 30), date(2026, 10, 31), date(2026, 11, 1)}, days)

	weeks := memorydigest.LevelWeek.ReadyPeriods(date(2026, 10, 15), now)
	require.Equal(t, []time.Time{date(2026, 10, 19), date(2026, 10, 26)}, weeks)

	months := memorydigest.LevelMonth.ReadyPeriods(date(2026, 9, 1), now)
	require.Equal(t, []time.Time{date(2026, 9, 1), date(2026, 10, 1)}, months)

	// On 1 November the last week of October is still open, so October is not ready.
	months = memorydigest.LevelMonth.ReadyPeriods(date(2026, 9, 1), date(2026, 11, 1))
	require.Equal(t, []time.Time{date(2026, 9, 1)}, months)
}

func TestParseLevel(t *testing.T) {
	t.Parallel()

	l, err := memorydigest.ParseLevel("week")
	require.NoError(t, err)
	require.Equal(t, memorydigest.LevelWeek, l)

	_, err = memorydigest.ParseLevel("year")
	require.Error(t, err)
}

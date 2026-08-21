package platformmcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveWindow_DefaultsAndClosedSet(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		requested string
		want      DiagnosticWindow
		wantSpan  time.Duration
		wantErr   bool
	}{
		{name: "empty defaults to a day", requested: "", want: DiagnosticWindowLastDay, wantSpan: 24 * time.Hour},
		{name: "hour", requested: "1h", want: DiagnosticWindowLastHour, wantSpan: time.Hour},
		{name: "week", requested: "7d", want: DiagnosticWindowLastWeek, wantSpan: 7 * 24 * time.Hour},
		{name: "month", requested: "30d", want: DiagnosticWindowLastMonth, wantSpan: 30 * 24 * time.Hour},
		{name: "case and space tolerated", requested: " 24H ", want: DiagnosticWindowLastDay, wantSpan: 24 * time.Hour},
		// Refused, not clamped: a caller must never receive a different window
		// than the one it believes it asked for.
		{name: "arbitrary duration refused", requested: "90m", wantErr: true},
		{name: "timestamp grammar refused", requested: "2026-08-01T00:00:00Z", wantErr: true},
		{name: "year refused", requested: "365d", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			window, err := resolveWindow(test.requested, now, overviewWindowSpec)
			if test.wantErr {
				require.ErrorIs(t, err, ErrDiagnosticWindowInvalid)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, window.Window)
			require.Equal(t, test.wantSpan, window.end.Sub(window.start))
			require.Equal(t, now, window.end)
			require.Equal(t, now.Add(-test.wantSpan).Format(time.RFC3339), window.From)
			require.Equal(t, now.Format(time.RFC3339), window.To)
		})
	}
}

func TestNewDataEnvelope_ClassifiesFreshness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("24h", now, overviewWindowSpec)
	require.NoError(t, err)

	tests := []struct {
		name          string
		watermark     time.Time
		want          Freshness
		wantThrough   bool
		wantThroughAt string
	}{
		{
			// Absence of observations is its own answer. Reporting it as fresh
			// would let a caller read an empty result as a healthy one.
			name:      "no watermark is unavailable",
			watermark: time.Time{},
			want:      FreshnessUnavailable,
		},
		{
			name:        "watermark inside the threshold is fresh",
			watermark:   now.Add(-2 * time.Minute),
			want:        FreshnessCurrent,
			wantThrough: true,
		},
		{
			name:        "watermark past the threshold is stale",
			watermark:   now.Add(-30 * time.Minute),
			want:        FreshnessStale,
			wantThrough: true,
		},
		{
			name:        "watermark exactly at the threshold is still fresh",
			watermark:   now.Add(-StaleWatermarkThreshold),
			want:        FreshnessCurrent,
			wantThrough: true,
		},
		{
			// A watermark at or past the window's end means the window is fully
			// covered, so a read about a completed interval is not stale merely
			// for being about the past.
			name:        "watermark at the window end is fresh",
			watermark:   window.end,
			want:        FreshnessCurrent,
			wantThrough: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			envelope := newDataEnvelope(now, test.watermark, window)
			require.Equal(t, test.want, envelope.Freshness)
			// no_observations is stated positively beside freshness, because
			// "unavailable" describes the pipeline while this describes the
			// result, and neither may be read as evidence of health.
			require.Equal(t, test.watermark.IsZero(), envelope.NoObservations)
			require.Equal(t, now.Format(time.RFC3339), envelope.QueriedAt)
			require.Equal(t, window, envelope.ResolvedWindow)
			if test.wantThrough {
				require.Equal(t, test.watermark.Format(time.RFC3339), envelope.DataThrough)
			} else {
				require.Empty(t, envelope.DataThrough)
			}
		})
	}
}

// TestResolveWindow_AdvertisedBoundsMatchTheQueriedBounds pins that the window
// a caller is told about is exactly the interval that was read. Formatting a
// sub-second now as RFC3339 would round the boundary away, so the advertised
// window and the nanosecond bounds behind it would silently disagree.
func TestResolveWindow_AdvertisedBoundsMatchTheQueriedBounds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 987_654_321, time.UTC)
	window, err := resolveWindow("1h", now, overviewWindowSpec)
	require.NoError(t, err)

	require.Equal(t, window.start.Format(time.RFC3339), window.From)
	require.Equal(t, window.end.Format(time.RFC3339), window.To)
	require.Equal(t, time.Hour, window.end.Sub(window.start))
	require.Zero(t, window.end.Nanosecond())
}

// TestWatermarkTime_ZeroIsNotTheEpoch pins that an absent watermark stays
// absent. Treating 0 as a real time would report every empty scope as data
// through 1970 and mark it stale rather than unobserved.
func TestWatermarkTime_ZeroIsNotTheEpoch(t *testing.T) {
	t.Parallel()

	require.True(t, watermarkTime(0).IsZero())
	require.True(t, watermarkTime(-1).IsZero())
	require.Equal(t, time.Unix(0, 1_700_000_000_000_000_000).UTC(), watermarkTime(1_700_000_000_000_000_000))
}

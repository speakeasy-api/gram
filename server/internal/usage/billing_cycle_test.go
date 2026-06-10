package usage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usage"
)

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestCurrentBillingCycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		now       time.Time
		anchorDay int
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "default anchor mid month",
			now:       time.Date(2026, time.June, 10, 15, 30, 0, 0, time.UTC),
			anchorDay: 1,
			wantStart: date(2026, time.June, 1),
			wantEnd:   date(2026, time.July, 1),
		},
		{
			name:      "anchor day later than now steps back a month",
			now:       time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC),
			anchorDay: 15,
			wantStart: date(2026, time.May, 15),
			wantEnd:   date(2026, time.June, 15),
		},
		{
			name:      "anchor day earlier than now stays in current month",
			now:       time.Date(2026, time.June, 20, 0, 0, 0, 0, time.UTC),
			anchorDay: 15,
			wantStart: date(2026, time.June, 15),
			wantEnd:   date(2026, time.July, 15),
		},
		{
			name:      "now exactly on anchor day starts new cycle",
			now:       date(2026, time.June, 15),
			anchorDay: 15,
			wantStart: date(2026, time.June, 15),
			wantEnd:   date(2026, time.July, 15),
		},
		{
			name:      "anchor 31 clamps to non-leap february",
			now:       time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
			anchorDay: 31,
			wantStart: date(2026, time.February, 28),
			wantEnd:   date(2026, time.March, 31),
		},
		{
			name:      "anchor 31 clamps to leap february",
			now:       time.Date(2028, time.March, 10, 0, 0, 0, 0, time.UTC),
			anchorDay: 31,
			wantStart: date(2028, time.February, 29),
			wantEnd:   date(2028, time.March, 31),
		},
		{
			name:      "anchor 31 in january does not normalize into march",
			now:       time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC),
			anchorDay: 31,
			wantStart: date(2026, time.January, 31),
			wantEnd:   date(2026, time.February, 28),
		},
		{
			name:      "year boundary steps back into december",
			now:       time.Date(2027, time.January, 3, 0, 0, 0, 0, time.UTC),
			anchorDay: 15,
			wantStart: date(2026, time.December, 15),
			wantEnd:   date(2027, time.January, 15),
		},
		{
			name:      "anchor 30 clamps february and recovers in march",
			now:       time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
			anchorDay: 30,
			wantStart: date(2026, time.February, 28),
			wantEnd:   date(2026, time.March, 30),
		},
		{
			name:      "out of range anchor falls back to first of month",
			now:       time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC),
			anchorDay: 0,
			wantStart: date(2026, time.June, 1),
			wantEnd:   date(2026, time.July, 1),
		},
		{
			name:      "non utc now is normalized to utc",
			now:       time.Date(2026, time.June, 1, 5, 0, 0, 0, time.FixedZone("UTC+10", 10*60*60)),
			anchorDay: 1,
			wantStart: date(2026, time.May, 1),
			wantEnd:   date(2026, time.June, 1),
		},
	}

	for _, tt := range tests {
		start, end := usage.CurrentBillingCycle(tt.now, tt.anchorDay)
		require.Equal(t, tt.wantStart, start, "%s: start", tt.name)
		require.Equal(t, tt.wantEnd, end, "%s: end", tt.name)
	}
}

func TestBillingCycles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)
	cycles := usage.BillingCycles(now, 15, 4)

	require.Equal(t, []usage.BillingCyclePeriod{
		{Start: date(2026, time.February, 15), End: date(2026, time.March, 15)},
		{Start: date(2026, time.March, 15), End: date(2026, time.April, 15)},
		{Start: date(2026, time.April, 15), End: date(2026, time.May, 15)},
		{Start: date(2026, time.May, 15), End: date(2026, time.June, 15)},
	}, cycles)
}

func TestBillingCycles_Anchor31ClampsShortMonths(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC)
	cycles := usage.BillingCycles(now, 31, 4)

	require.Equal(t, []usage.BillingCyclePeriod{
		{Start: date(2025, time.December, 31), End: date(2026, time.January, 31)},
		{Start: date(2026, time.January, 31), End: date(2026, time.February, 28)},
		{Start: date(2026, time.February, 28), End: date(2026, time.March, 31)},
		{Start: date(2026, time.March, 31), End: date(2026, time.April, 30)},
	}, cycles)
}

func TestBillingCycles_ContiguousAcrossYearBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2027, time.February, 1, 0, 0, 0, 0, time.UTC)
	cycles := usage.BillingCycles(now, 1, 12)

	require.Len(t, cycles, 12)
	require.Equal(t, date(2026, time.March, 1), cycles[0].Start)
	require.Equal(t, date(2027, time.February, 1), cycles[len(cycles)-1].Start)
	for i := 1; i < len(cycles); i++ {
		require.Equal(t, cycles[i-1].End, cycles[i].Start, "cycles must be contiguous")
	}
}

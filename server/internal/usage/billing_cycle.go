package usage

import "time"

// CurrentBillingCycle returns the [start, end) boundaries of the billing
// cycle containing now, anchored at anchorDay (1-31) at 00:00 UTC. Anchor
// days beyond a month's length clamp to that month's last day, so an anchor
// of 31 yields cycles starting Jan 31, Feb 28 (or 29), Mar 31, and so on.
// Anchor days outside 1-31 are treated as 1.
func CurrentBillingCycle(now time.Time, anchorDay int) (start time.Time, end time.Time) {
	if anchorDay < 1 || anchorDay > 31 {
		anchorDay = 1
	}

	now = now.UTC()

	start = anchoredCycleStart(now.Year(), now.Month(), anchorDay)
	if start.After(now) {
		start = anchoredCycleStart(now.Year(), now.Month()-1, anchorDay)
	}

	// Each boundary is derived from the raw anchor day against its own month
	// rather than start.AddDate(0, 1, 0), which would normalize Jan 31 into
	// early March.
	end = anchoredCycleStart(start.Year(), start.Month()+1, anchorDay)

	return start, end
}

// anchoredCycleStart returns 00:00 UTC on the anchor day of the given month,
// clamped to the month's last day. Out-of-range months are normalized by
// time.Date (e.g. month 0 becomes December of the prior year).
func anchoredCycleStart(year int, month time.Month, anchorDay int) time.Time {
	// Day 0 of the next month normalizes to the last day of this month.
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()

	return time.Date(year, month, min(anchorDay, lastDay), 0, 0, 0, 0, time.UTC)
}

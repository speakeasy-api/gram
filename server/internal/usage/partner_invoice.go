package usage

import "time"

// partnerMonthlyPeriod validates Stripe's original monthly anchor, including
// end-of-month clamping. An externally changed anchor is not silently rebucketed.
func partnerMonthlyPeriod(anchor, start, end time.Time) bool {
	anchor, start, end = anchor.UTC(), start.UTC(), end.UTC()
	if start.Before(anchor) || !end.After(start) {
		return false
	}
	monthIndex := func(t time.Time) int { return t.Year()*12 + int(t.Month()) }
	if monthIndex(end)-monthIndex(start) != 1 {
		return false
	}
	boundary := func(t time.Time) bool {
		day := min(anchor.Day(), time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day())
		expected := time.Date(t.Year(), t.Month(), day, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
		return t.Equal(expected)
	}
	return boundary(start) && boundary(end)
}

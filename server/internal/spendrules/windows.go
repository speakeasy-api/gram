package spendrules

import (
	"fmt"
	"time"
)

const (
	WindowDaily   = "daily"
	WindowWeekly  = "weekly"
	WindowMonthly = "monthly"
)

// WindowBounds returns the UTC calendar window containing now for the given
// window kind: daily windows reset at midnight UTC, weekly windows on Monday
// 00:00 UTC, monthly windows on the 1st 00:00 UTC. The end bound is exclusive.
func WindowBounds(kind string, now time.Time) (start time.Time, end time.Time, err error) {
	now = now.UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	switch kind {
	case WindowDaily:
		return midnight, midnight.AddDate(0, 0, 1), nil
	case WindowWeekly:
		// time.Weekday is Sunday-based; shift so Monday starts the week.
		offset := (int(now.Weekday()) + 6) % 7
		start = midnight.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 7), nil
	case WindowMonthly:
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unknown window kind %q", kind)
	}
}

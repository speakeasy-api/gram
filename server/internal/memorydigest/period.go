// Package memorydigest holds the clock and assembly rules for team memory
// digests: which periods are closed, which digest covers which children,
// what the rollup prompt contains, and how the recall block is cut to its
// token budget. It does no I/O, so the background rollup job and the
// Platform MCP tools share one definition of the digest tree.
package memorydigest

import (
	"fmt"
	"time"
)

// Level is the granularity of a digest. Day digests fold chat summaries and
// memos, week digests fold day digests, and month digests fold week digests.
type Level string

const (
	LevelDay   Level = "day"
	LevelWeek  Level = "week"
	LevelMonth Level = "month"
)

// ParseLevel validates a stored or user-supplied level.
func ParseLevel(s string) (Level, error) {
	switch l := Level(s); l {
	case LevelDay, LevelWeek, LevelMonth:
		return l, nil
	default:
		return "", fmt.Errorf("unknown digest level %q", s)
	}
}

// ChildLevel is the level a digest folds. A day digest has no child digests:
// its children are chats and memos, so ok is false.
func (l Level) ChildLevel() (child Level, ok bool) {
	switch l {
	case LevelWeek:
		return LevelDay, true
	case LevelMonth:
		return LevelWeek, true
	default:
		return "", false
	}
}

// PeriodStart returns the start of the period at this level that contains t,
// in UTC. Weeks start on Monday. Digests are keyed on this value.
func (l Level) PeriodStart(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	switch l {
	case LevelWeek:
		// time.Weekday counts from Sunday; shift so Monday is 0.
		offset := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -offset)
	case LevelMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return day
	}
}

// PeriodEnd returns the exclusive end of the period that starts at start.
func (l Level) PeriodEnd(start time.Time) time.Time {
	switch l {
	case LevelWeek:
		return start.AddDate(0, 0, 7)
	case LevelMonth:
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(0, 0, 1)
	}
}

// ReadyAt is when the digest for the period starting at start can be
// written. Day and week digests are ready when their period ends. A month
// digest folds the weeks that start inside it, and the last of those can
// run up to six days into the next month, so the month waits for that week
// to close. This keeps every digest written once, from closed children only.
func (l Level) ReadyAt(start time.Time) time.Time {
	end := l.PeriodEnd(start)
	if l != LevelMonth {
		return end
	}
	lastWeek := LevelWeek.PeriodStart(end.AddDate(0, 0, -1))
	return LevelWeek.PeriodEnd(lastWeek)
}

// ParentStart returns the start of the parent period that a digest at this
// level rolls into. A week belongs to the month its Monday falls in, so each
// week has exactly one parent and the digest tree never double-counts.
func (l Level) ParentStart(start time.Time) (parent time.Time, ok bool) {
	switch l {
	case LevelDay:
		return LevelWeek.PeriodStart(start), true
	case LevelWeek:
		return LevelMonth.PeriodStart(start), true
	default:
		return time.Time{}, false
	}
}

// ChildStarts lists the period starts of the child digests a digest at this
// level folds, in chronological order. It is empty for day digests.
func (l Level) ChildStarts(start time.Time) []time.Time {
	child, ok := l.ChildLevel()
	if !ok {
		return nil
	}
	var starts []time.Time
	switch l {
	case LevelWeek:
		for d := range 7 {
			starts = append(starts, start.AddDate(0, 0, d))
		}
	case LevelMonth:
		end := l.PeriodEnd(start)
		for w := child.PeriodStart(start); w.Before(end); w = child.PeriodEnd(w) {
			if !w.Before(start) {
				starts = append(starts, w)
			}
		}
	case LevelDay:
		// Unreachable: ChildLevel reports no children for days.
	}
	return starts
}

// ReadyPeriods lists the periods at this level that are ready by now and
// start at or after since, oldest first. The rollup job writes any of these
// that have no digest yet, so a missed run catches up on its next tick.
func (l Level) ReadyPeriods(since, now time.Time) []time.Time {
	var starts []time.Time
	for p := l.PeriodStart(since); !l.ReadyAt(p).After(now); p = l.PeriodEnd(p) {
		if !p.Before(since) {
			starts = append(starts, p)
		}
	}
	return starts
}

package access

import (
	"errors"
	"fmt"
	"time"
)

// Durations a requester can ask for, counted from approval. The approver
// still picks the window.
const (
	RequestedDuration1h = "1h"
	RequestedDuration2h = "2h"
	RequestedDuration4h = "4h"
)

// requestedDurations maps each duration a requester can ask for to its
// length.
var requestedDurations = map[string]time.Duration{
	RequestedDuration1h: time.Hour,
	RequestedDuration2h: 2 * time.Hour,
	RequestedDuration4h: 4 * time.Hour,
}

// MinEndOfDayLead hides or refuses End of day when the day ends sooner than
// this. A temporary grant needs at least five minutes, so ten leaves room to
// decide.
const MinEndOfDayLead = 10 * time.Minute

var (
	// errDurationAndUntil reports a request that asks for both a duration and
	// a deadline.
	errDurationAndUntil = errors.New("send either duration or requested_until, not both")
	// errRequestedUntilPast reports a deadline that has already passed.
	errRequestedUntilPast = errors.New("requested_until must be in the future")
	// errRequestedUntilTooFar reports a deadline further away than a
	// temporary grant can last.
	errRequestedUntilTooFar = errors.New("requested_until must be at most 24 hours away")
)

// EndOfDay is 23:59 today in loc, or zero when that is less than
// MinEndOfDayLead away or, on a day that gains an hour, further away than a
// temporary grant can last.
func EndOfDay(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	end := time.Date(local.Year(), local.Month(), local.Day(), 23, 59, 0, 0, loc)
	if lead := end.Sub(now); lead < MinEndOfDayLead || lead > TemporaryGrantMaxDuration {
		return time.Time{}
	}
	return end
}

// RequestedAccessWindow reads what a requester asked for: a fixed duration
// counted from approval (1h, 2h or 4h), or a deadline their own client worked
// out, such as the end of their day, as an RFC 3339 timestamp. Either may be
// nil; both at once is refused. The deadline is returned in UTC, and must be
// in the future and at most 24 hours from now.
func RequestedAccessWindow(duration, requestedUntil *string, now time.Time) (time.Duration, time.Time, error) {
	if duration != nil && requestedUntil != nil {
		return 0, time.Time{}, errDurationAndUntil
	}
	if duration != nil {
		length, ok := requestedDurations[*duration]
		if !ok {
			return 0, time.Time{}, fmt.Errorf("unknown duration %q", *duration)
		}
		return length, time.Time{}, nil
	}
	if requestedUntil != nil {
		until, err := time.Parse(time.RFC3339, *requestedUntil)
		if err != nil {
			return 0, time.Time{}, errors.New("requested_until must be an RFC 3339 timestamp")
		}
		if !until.After(now) {
			return 0, time.Time{}, errRequestedUntilPast
		}
		if until.Sub(now) > TemporaryGrantMaxDuration {
			return 0, time.Time{}, errRequestedUntilTooFar
		}
		return 0, until.UTC(), nil
	}
	return 0, time.Time{}, nil
}

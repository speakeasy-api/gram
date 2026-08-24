package httpcache

import (
	"math"
	"net/http"
	"time"
)

// responseAge is RFC 9111 §4.2.3's current_age: the larger of the Age header
// and the apparent age the Date header implies. Taking the larger of the two
// matters because they fail in different ways — an intermediary can replay a
// stale body without adding Age, and an origin can send an Age larger than
// its own Date suggests — and trusting only Age would hand a full lifetime to
// the first case.
//
// It approximates rather than reproduces the RFC's current_age, which also
// adds the request delay and the time the response has since spent resident
// in the cache. Resident time is zero by construction here (the absolute
// expiry is derived from this the moment the fetch returns) and the request
// delay is bounded by the consumer's fetch timeout, so both are far below the
// Min floor every result is clamped to.
//
// An absent or malformed Age reads as zero, and a Date in the future (a
// skewed origin clock) yields a negative apparent age that never wins the
// comparison, so the result is never negative.
func responseAge(header http.Header, now time.Time) time.Duration {
	age := headerAge(header)
	if date, err := http.ParseTime(header.Get("Date")); err == nil {
		age = max(age, now.Sub(date))
	}
	return age
}

// headerAge reads the Age header.
//
// An age too large to represent is treated as the opposite of an unparseable
// lifetime. A lifetime we cannot parse means the host expressed no opinion,
// so the default applies; an age we cannot parse because it is astronomically
// large is the host saying the response is ancient, so it saturates and the
// subtraction drives the TTL to the floor. Reading it as zero instead would
// grant a document the host called stale its full lifetime.
func headerAge(header http.Header) time.Duration {
	raw := header.Get("Age")
	if raw == "" {
		return 0
	}
	if age, ok := deltaSeconds(raw); ok {
		return age
	}
	if _, wellFormed := deltaSecondsDigits(raw); wellFormed {
		return time.Duration(math.MaxInt64)
	}
	return 0
}

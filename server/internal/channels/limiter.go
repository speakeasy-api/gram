package channels

import (
	"sync"
	"time"
)

// burstLimiter is a token bucket per writing identity. It bounds one caller's
// write burst against a replica; it is deliberately not a fleet-wide quota,
// because Channels must keep working when no shared limiter store is wired in.
// Callers that need a fleet-wide cap put one in front of the handler.
type burstLimiter struct {
	mu        sync.Mutex
	rate      float64
	burst     float64
	buckets   map[string]*bucket
	sweptAt   time.Time
	sweepIdle time.Duration
}

type bucket struct {
	tokens float64
	seenAt time.Time
}

func newBurstLimiter(perMinute, burst int) *burstLimiter {
	if perMinute < 1 {
		perMinute = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &burstLimiter{
		mu:        sync.Mutex{},
		rate:      float64(perMinute) / 60,
		burst:     float64(burst),
		buckets:   map[string]*bucket{},
		sweptAt:   time.Time{},
		sweepIdle: 10 * time.Minute,
	}
}

// allow spends one token for key, refilling it for the time since the last
// call. It reports whether the caller may write now.
func (l *burstLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	held, ok := l.buckets[key]
	if !ok {
		held = &bucket{tokens: l.burst, seenAt: now}
		l.buckets[key] = held
	}
	if elapsed := now.Sub(held.seenAt); elapsed > 0 {
		held.tokens = min(l.burst, held.tokens+elapsed.Seconds()*l.rate)
	}
	held.seenAt = now
	if held.tokens < 1 {
		return false
	}
	held.tokens--
	return true
}

// sweep drops buckets nobody has touched for a while so a long-lived process
// does not accumulate one entry per identity that ever wrote.
func (l *burstLimiter) sweep(now time.Time) {
	if now.Sub(l.sweptAt) < l.sweepIdle {
		return
	}
	l.sweptAt = now
	for key, held := range l.buckets {
		if now.Sub(held.seenAt) > l.sweepIdle {
			delete(l.buckets, key)
		}
	}
}

package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// memorySweepInterval is how often idle buckets are reaped. A bucket idle this
// long has refilled to full, so dropping it loses no state.
const memorySweepInterval = 5 * time.Minute

// memoryStore is a per-process Store backed by golang.org/x/time/rate, with lazy
// GC of idle buckets — the pattern riskjudge, pijudge, and the assistant
// bootstrap handler each hand-rolled before this package. Use it for
// single-replica or test paths; a multi-replica fleet needs the Redis Store to
// share one cap.
type memoryStore struct {
	mu        sync.Mutex
	buckets   map[string]*memoryBucket
	now       func() time.Time
	lastSweep time.Time
}

type memoryBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// MemoryOption configures a memory Store.
type MemoryOption func(*memoryStore)

// WithClock overrides the time source. Tests advance it to exercise refill
// without real waits.
func WithClock(now func() time.Time) MemoryOption {
	return func(s *memoryStore) {
		s.now = now
	}
}

// NewMemoryStore returns an in-process Store. Infallible.
func NewMemoryStore(opts ...MemoryOption) Store {
	s := &memoryStore{
		mu:        sync.Mutex{},
		buckets:   map[string]*memoryBucket{},
		now:       time.Now,
		lastSweep: time.Time{},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.lastSweep = s.now()
	return s
}

func (s *memoryStore) take(_ context.Context, key string, r Rate, n int) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.sweep(now)

	b, ok := s.buckets[key]
	if !ok {
		b = &memoryBucket{
			limiter:  rate.NewLimiter(rate.Limit(r.tokensPerSecond()), r.Burst),
			lastSeen: now,
		}
		s.buckets[key] = b
	}
	b.lastSeen = now

	// AllowN consumes n tokens on success and leaves the bucket untouched on
	// failure, so we can compute RetryAfter from the post-check token count
	// without reserving into the future.
	if b.limiter.AllowN(now, n) {
		return Result{Allowed: true, Remaining: int(b.limiter.TokensAt(now)), RetryAfter: 0}, nil
	}

	tokens := b.limiter.TokensAt(now)
	if n > r.Burst {
		// Can never be satisfied — no finite wait helps.
		return Result{Allowed: false, Remaining: int(tokens), RetryAfter: 0}, nil
	}
	deficit := float64(n) - tokens
	retryAfter := time.Duration(deficit / r.tokensPerSecond() * float64(time.Second))
	return Result{Allowed: false, Remaining: int(tokens), RetryAfter: retryAfter}, nil
}

// sweep drops buckets idle past memorySweepInterval, bounding memory across many
// keys without a background goroutine. Caller holds s.mu.
func (s *memoryStore) sweep(now time.Time) {
	if now.Sub(s.lastSweep) <= memorySweepInterval {
		return
	}
	for k, b := range s.buckets {
		if now.Sub(b.lastSeen) > memorySweepInterval {
			delete(s.buckets, k)
		}
	}
	s.lastSweep = now
}

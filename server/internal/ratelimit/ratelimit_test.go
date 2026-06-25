package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// baseTime is a fixed instant so clock-driven tests are deterministic.
var baseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestRateHelpers(t *testing.T) {
	t.Parallel()

	require.Equal(t, Rate{Tokens: 60, Interval: time.Minute, Burst: 60}, PerMinute(60))
	require.Equal(t, Rate{Tokens: 5, Interval: time.Second, Burst: 5}, PerSecond(5))
	require.Equal(t, Rate{Tokens: 280, Interval: time.Minute, Burst: 60}, PerMinute(280).WithBurst(60))

	require.True(t, PerMinute(1).Valid())
	require.False(t, Rate{Tokens: 0, Interval: time.Minute, Burst: 1}.Valid())
	require.False(t, Rate{Tokens: 1, Interval: 0, Burst: 1}.Valid())
	require.False(t, Rate{Tokens: 1, Interval: time.Minute, Burst: 0}.Valid())
	require.InDelta(t, 1.0, PerMinute(60).tokensPerSecond(), 1e-9)
}

func TestMemoryStoreBurstThenThrottle(t *testing.T) {
	t.Parallel()

	clk := &fakeClock{t: baseTime}
	// 60/min == 1 token/sec, burst 5: a momentarily idle key spends 5 at once.
	limiter := New(NewMemoryStore(WithClock(clk.now)), "burst", Rate{Tokens: 60, Interval: time.Minute, Burst: 5})

	for i := range 5 {
		res, err := limiter.Allow(t.Context(), "k")
		require.NoError(t, err)
		require.True(t, res.Allowed, "token %d should be allowed", i)
		require.Equal(t, 4-i, res.Remaining)
	}

	res, err := limiter.Allow(t.Context(), "k")
	require.NoError(t, err)
	require.False(t, res.Allowed, "6th call exhausts the burst")
	require.Equal(t, 0, res.Remaining)
	require.InDelta(t, float64(time.Second), float64(res.RetryAfter), float64(50*time.Millisecond))
}

func TestMemoryStoreRefillsOverTime(t *testing.T) {
	t.Parallel()

	clk := &fakeClock{t: baseTime}
	limiter := New(NewMemoryStore(WithClock(clk.now)), "refill", Rate{Tokens: 60, Interval: time.Minute, Burst: 5})

	for range 5 {
		res, err := limiter.Allow(t.Context(), "k")
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}
	denied, err := limiter.Allow(t.Context(), "k")
	require.NoError(t, err)
	require.False(t, denied.Allowed)

	// One token regenerates after a second.
	clk.advance(time.Second)
	res, err := limiter.Allow(t.Context(), "k")
	require.NoError(t, err)
	require.True(t, res.Allowed, "a token refills after 1s")

	// Long idle refills only up to the burst, never beyond.
	clk.advance(time.Hour)
	allowed := 0
	for range 10 {
		r, err := limiter.Allow(t.Context(), "k")
		require.NoError(t, err)
		if r.Allowed {
			allowed++
		}
	}
	require.Equal(t, 5, allowed, "refill is capped at the burst")
}

func TestAllowNChargesBatch(t *testing.T) {
	t.Parallel()

	clk := &fakeClock{t: baseTime}
	limiter := New(NewMemoryStore(WithClock(clk.now)), "batch", Rate{Tokens: 60, Interval: time.Minute, Burst: 10})

	res, err := limiter.AllowN(t.Context(), "k", 7)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	require.Equal(t, 3, res.Remaining)

	res, err = limiter.AllowN(t.Context(), "k", 7)
	require.NoError(t, err)
	require.False(t, res.Allowed, "only 3 tokens left, cannot charge 7")
	require.Greater(t, res.RetryAfter, time.Duration(0))
}

func TestAllowNBeyondBurstNeverSucceeds(t *testing.T) {
	t.Parallel()

	limiter := New(NewMemoryStore(), "huge", Rate{Tokens: 60, Interval: time.Minute, Burst: 5})

	res, err := limiter.AllowN(t.Context(), "k", 6)
	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Equal(t, time.Duration(0), res.RetryAfter, "no finite wait can satisfy n > burst")
}

func TestLimiterNamespaceIsolation(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	a := New(store, "service-a", Rate{Tokens: 60, Interval: time.Minute, Burst: 1})
	b := New(store, "service-b", Rate{Tokens: 60, Interval: time.Minute, Burst: 1})

	// Same key "shared", but different limiter names => independent buckets.
	res, err := a.Allow(t.Context(), "shared")
	require.NoError(t, err)
	require.True(t, res.Allowed)

	res, err = a.Allow(t.Context(), "shared")
	require.NoError(t, err)
	require.False(t, res.Allowed, "service-a bucket is now empty")

	res, err = b.Allow(t.Context(), "shared")
	require.NoError(t, err)
	require.True(t, res.Allowed, "service-b bucket is untouched")
}

func TestLimiterKeyIsolation(t *testing.T) {
	t.Parallel()

	limiter := New(NewMemoryStore(), "svc", Rate{Tokens: 60, Interval: time.Minute, Burst: 1})

	res, err := limiter.Allow(t.Context(), "org-1")
	require.NoError(t, err)
	require.True(t, res.Allowed)

	res, err = limiter.Allow(t.Context(), "org-2")
	require.NoError(t, err)
	require.True(t, res.Allowed, "a different key has its own bucket")
}

func TestLimiterInvalidRateErrors(t *testing.T) {
	t.Parallel()

	limiter := New(NewMemoryStore(), "bad", Rate{Tokens: 0, Interval: time.Minute, Burst: 0})

	res, err := limiter.Allow(t.Context(), "k")
	require.Error(t, err)
	require.False(t, res.Allowed)
}

func TestMemoryStoreSweepsIdleBuckets(t *testing.T) {
	t.Parallel()

	clk := &fakeClock{t: baseTime}
	store := NewMemoryStore(WithClock(clk.now))
	limiter := New(store, "sweep", Rate{Tokens: 60, Interval: time.Minute, Burst: 1})

	_, err := limiter.Allow(t.Context(), "idle")
	require.NoError(t, err)

	ms, ok := store.(*memoryStore)
	require.True(t, ok)
	require.Len(t, ms.buckets, 1)

	// Past the sweep interval, a touch on a different key reaps the idle bucket.
	clk.advance(memorySweepInterval + time.Minute)
	_, err = limiter.Allow(t.Context(), "fresh")
	require.NoError(t, err)

	_, idleStillPresent := ms.buckets[keyPrefix+"sweep:idle"]
	require.False(t, idleStillPresent, "idle bucket should be swept")
	_, freshPresent := ms.buckets[keyPrefix+"sweep:fresh"]
	require.True(t, freshPresent)
}

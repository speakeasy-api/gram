package ratelimit

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newRedisStore(t *testing.T) Store {
	t.Helper()
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return NewRedisStore(client)
}

func TestRedisStoreBurstThenThrottle(t *testing.T) {
	t.Parallel()

	limiter := New(newRedisStore(t), t.Name(), Rate{Tokens: 60, Interval: time.Minute, Burst: 5})

	for i := range 5 {
		res, err := limiter.Allow(t.Context(), "k")
		require.NoError(t, err)
		require.True(t, res.Allowed, "token %d should be allowed", i)
	}

	res, err := limiter.Allow(t.Context(), "k")
	require.NoError(t, err)
	require.False(t, res.Allowed, "burst is exhausted")
	require.Greater(t, res.RetryAfter, time.Duration(0))
}

// TestRedisStoreConcurrentRespectsBurst is the distributed-correctness test: a
// burst's worth of tokens, hammered concurrently, is handed out exactly once —
// the atomic guarantee the per-replica in-memory limiters could not give.
func TestRedisStoreConcurrentRespectsBurst(t *testing.T) {
	t.Parallel()

	const burst = 10
	// 10/min == one token per 6s, so no meaningful refill during the burst.
	limiter := New(newRedisStore(t), t.Name(), Rate{Tokens: 10, Interval: time.Minute, Burst: burst})

	var (
		mu      sync.Mutex
		allowed int
		errs    []error
	)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := limiter.Allow(t.Context(), "hot")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				errs = append(errs, err)
			case res.Allowed:
				allowed++
			}
		}()
	}
	wg.Wait()

	require.Empty(t, errs, "no Allow call should error")
	require.Equal(t, burst, allowed, "exactly burst tokens granted across all goroutines")
}

func TestLimiterInvalidRateErrors(t *testing.T) {
	t.Parallel()

	limiter := New(newRedisStore(t), t.Name(), Rate{Tokens: 0, Interval: time.Minute, Burst: 0})
	res, err := limiter.Allow(t.Context(), "k")
	require.Error(t, err)
	require.False(t, res.Allowed)
}

func TestAllowNRejectsNonPositive(t *testing.T) {
	t.Parallel()

	limiter := New(newRedisStore(t), t.Name(), Rate{Tokens: 60, Interval: time.Minute, Burst: 10})
	for _, n := range []int{0, -1} {
		res, err := limiter.AllowN(t.Context(), "k", n)
		require.Error(t, err, "n=%d must be rejected", n)
		require.False(t, res.Allowed)
	}
}

func TestAllowNBeyondBurstNeverSucceeds(t *testing.T) {
	t.Parallel()

	limiter := New(newRedisStore(t), t.Name(), Rate{Tokens: 60, Interval: time.Minute, Burst: 5})
	res, err := limiter.AllowN(t.Context(), "k", 6)
	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Equal(t, time.Duration(0), res.RetryAfter, "n > burst can never be satisfied")
}

func TestRedisStoreNamespaceIsolation(t *testing.T) {
	t.Parallel()

	store := newRedisStore(t)
	a := New(store, t.Name()+"-a", Rate{Tokens: 60, Interval: time.Minute, Burst: 1})
	b := New(store, t.Name()+"-b", Rate{Tokens: 60, Interval: time.Minute, Burst: 1})

	res, err := a.Allow(t.Context(), "shared")
	require.NoError(t, err)
	require.True(t, res.Allowed)

	res, err = a.Allow(t.Context(), "shared")
	require.NoError(t, err)
	require.False(t, res.Allowed, "limiter a is exhausted")

	res, err = b.Allow(t.Context(), "shared")
	require.NoError(t, err)
	require.True(t, res.Allowed, "limiter b shares the key but not the bucket")
}

func TestRedisStoreAllowNChargesBatch(t *testing.T) {
	t.Parallel()

	limiter := New(newRedisStore(t), t.Name(), Rate{Tokens: 60, Interval: time.Minute, Burst: 10})

	res, err := limiter.AllowN(t.Context(), "k", 8)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	require.Equal(t, 2, res.Remaining)

	res, err = limiter.AllowN(t.Context(), "k", 8)
	require.NoError(t, err)
	require.False(t, res.Allowed, "only 2 tokens remain, cannot charge 8")
}

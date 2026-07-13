package guardian_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/time/rate"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestInProcLimiter_AllowN(t *testing.T) {
	t.Parallel()

	l := guardian.NewInProcLimiter(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	key := guardian.NewPartition("egress", "org-1")
	limit := guardian.PerSecond(10)

	// Burst of 10 should admit 10 single requests immediately.
	for j := range 10 {
		res, err := l.AllowN(t.Context(), key, limit, 1)
		require.NoError(t, err)
		require.Equal(t, 1, res.Allowed, "request %d should be allowed", j)
		require.Equal(t, time.Duration(-1), res.RetryAfter)
		require.InDelta(t, 10-(j+1), res.Remaining, 1)
	}

	// The 11th request must be denied with a positive RetryAfter.
	res, err := l.AllowN(t.Context(), key, limit, 1)
	require.NoError(t, err)
	require.Equal(t, 0, res.Allowed)
	require.Positive(t, res.RetryAfter)
	require.LessOrEqual(t, res.RetryAfter, 150*time.Millisecond, "10/sec refill means next token ~100ms away")
	require.Greater(t, res.ResetAfter, 900*time.Millisecond, "bucket should take ~1s to refill fully")

	// A denied request must not consume tokens: draining a fresh key twice in
	// a row with n > remaining should leave state intact.
	key2 := guardian.NewPartition("egress", "org-2")
	res, err = l.AllowN(t.Context(), key2, limit, 8)
	require.NoError(t, err)
	require.Equal(t, 8, res.Allowed)
	before := res.Remaining
	res, err = l.AllowN(t.Context(), key2, limit, 8)
	require.NoError(t, err)
	require.Equal(t, 0, res.Allowed)
	require.InDelta(t, before, res.Remaining, 1, "cancelled reservation should return tokens")

	// n > burst can never be satisfied: a permanent denial, not an error.
	// This must hold for any cost — including ones above math.MaxInt32,
	// which on 32-bit builds would fail the representability check if it
	// ran first.
	for _, n := range []uint32{11, math.MaxUint32} {
		res, err = l.AllowN(t.Context(), key, limit, n)
		require.NoError(t, err)
		require.Equal(t, 0, res.Allowed)
		require.Equal(t, rate.InfDuration, res.RetryAfter)
	}

	// Tenant keys must not collide with each other or the global key.
	require.NotEqual(t, key.String(), key2.String())
	require.NotEqual(t, guardian.NewPartition("egress").String(), key.String())

	// Changing the limit for an existing key takes effect: tokens carry over
	// (~2 left), but the ~48-token deficit refills at the new 100/sec rate
	// (~0.5s) instead of the old 10/sec rate (~5s).
	res, err = l.AllowN(t.Context(), key2, guardian.PerSecond(100), 50)
	require.NoError(t, err)
	require.Equal(t, 0, res.Allowed)
	require.Positive(t, res.RetryAfter)
	require.Less(t, res.RetryAfter, time.Second)
	require.Equal(t, uint32(100), res.Limit.Burst)
}

func TestInProcLimiter_BucketGauge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})

	l := guardian.NewInProcLimiter(testenv.NewLogger(t), meterProvider)
	limit := guardian.PerSecond(10)

	require.Empty(t, instanceGaugeByNamespace(t, reader, "gram.rate_limit.buckets"))

	// One bucket per distinct partition, attributed to its namespace.
	for _, key := range []guardian.Partition{
		guardian.NewPartition("egress", "org-1"),
		guardian.NewPartition("egress", "org-2"),
		guardian.NewPartition("chat", "org-1"),
	} {
		_, err := l.AllowN(t.Context(), key, limit, 1)
		require.NoError(t, err)
	}

	require.Equal(t,
		map[string]int64{"egress": 2, "chat": 1},
		instanceGaugeByNamespace(t, reader, "gram.rate_limit.buckets"),
	)

	// Repeat traffic on an existing partition, including with a changed
	// limit, reuses its bucket and does not grow the count.
	_, err := l.AllowN(t.Context(), guardian.NewPartition("egress", "org-1"), guardian.PerSecond(100), 1)
	require.NoError(t, err)

	require.Equal(t,
		map[string]int64{"egress": 2, "chat": 1},
		instanceGaugeByNamespace(t, reader, "gram.rate_limit.buckets"),
	)
}

func TestInProcLimiter_AllowN_InvalidLimit(t *testing.T) {
	t.Parallel()

	l := guardian.NewInProcLimiter(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	key := guardian.NewPartition("egress", "org-1")
	valid := guardian.PerSecond(10)

	for _, mutate := range []func(*guardian.Limit){
		func(l *guardian.Limit) { l.Rate = 0 },
		func(l *guardian.Limit) { l.Burst = 0 },
		func(l *guardian.Limit) { l.Period = 0 },
		func(l *guardian.Limit) { l.Period = -time.Second },
	} {
		limit := valid
		mutate(&limit)
		_, err := l.AllowN(t.Context(), key, limit, 1)
		require.Error(t, err)
	}
}

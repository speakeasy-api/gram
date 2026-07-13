package guardian_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRedisRateLimiter_AllowN(t *testing.T) {
	t.Parallel()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	l := guardian.NewRedisRateLimiter(testenv.NewLogger(t), testenv.NewMeterProvider(t), client)
	// Random key segment: redis bucket state outlives the test binary, so a
	// fixed key would fail on repeated runs (-count) against the same
	// container.
	key := guardian.NewPartition("egress", uuid.NewString())
	limit := guardian.PerHour(1)

	res, err := l.AllowN(t.Context(), key, limit, 1)
	require.NoError(t, err)
	require.Equal(t, 1, res.Allowed)

	res, err = l.AllowN(t.Context(), key, limit, 1)
	require.NoError(t, err)
	require.Equal(t, 0, res.Allowed)
	require.Positive(t, res.RetryAfter)

	// Other partitions are unaffected.
	other, err := l.AllowN(t.Context(), guardian.NewPartition("egress", uuid.NewString()), limit, 1)
	require.NoError(t, err)
	require.Equal(t, 1, other.Allowed)

	// n > burst can never be satisfied: the denial must be permanent rather
	// than reporting GCRA's finite retry-after, which would send Wait-based
	// retry loops spinning forever.
	res, err = l.AllowN(t.Context(), key, limit, 2)
	require.NoError(t, err)
	require.Equal(t, 0, res.Allowed)
	require.GreaterOrEqual(t, res.RetryAfter, rate.InfDuration)
}

func TestRedisRateLimiter_AllowN_InvalidLimit(t *testing.T) {
	t.Parallel()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	l := guardian.NewRedisRateLimiter(testenv.NewLogger(t), testenv.NewMeterProvider(t), client)
	key := guardian.NewPartition("egress", uuid.NewString())
	valid := guardian.PerSecond(10)

	// GCRA fails open on a zero emission interval, so malformed limits must
	// be rejected before they reach Redis.
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

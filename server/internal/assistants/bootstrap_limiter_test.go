package assistants

import (
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestBootstrapAggregateBoundsConcurrentThreadTraffic(t *testing.T) {
	t.Parallel()
	cache := miniredis.RunT(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cache.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := ratelimit.NewRedisStore(client)
	var service Service
	service.logger = testenv.NewLogger(t)
	service.bootstrapLimiter = ratelimit.New(store, "test-bootstrap-thread", ratelimit.PerMinute(bootstrapRatePerMin).WithBurst(bootstrapRateBurst))
	service.bootstrapAggregateLimiter = ratelimit.New(store, "test-bootstrap-aggregate", ratelimit.PerMinute(bootstrapAggregateRatePerMin).WithBurst(bootstrapAggregateBurst))
	assistant := uuid.New()
	// All 100 supported concurrent slots can bootstrap together. Additional
	// thread IDs do not create aggregate capacity; refill is bounded at 100/s.
	for wave := range 3 {
		cache.SetTime(now.Add(time.Duration(wave) * time.Second))
		var wg sync.WaitGroup
		results := make(chan error, bootstrapAggregateBurst)
		for range bootstrapAggregateBurst {
			wg.Go(func() { results <- service.allowBootstrap(t.Context(), assistant, uuid.New()) })
		}
		wg.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		require.Error(t, service.allowBootstrap(t.Context(), assistant, uuid.New()), "fresh thread IDs cannot bypass aggregate protection")
	}
	require.NoError(t, service.allowBootstrap(t.Context(), uuid.New(), uuid.New()), "aggregate buckets must remain assistant-scoped")
	// Legacy assistant-wide tokens keep their tighter 60-call bucket.
	legacy := uuid.New()
	for range bootstrapRateBurst {
		require.NoError(t, service.allowBootstrap(t.Context(), legacy, uuid.Nil))
	}
	require.Error(t, service.allowBootstrap(t.Context(), legacy, uuid.Nil))
}

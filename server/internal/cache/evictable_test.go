package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/redisrole"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newEvictablePair(t *testing.T, mode redisrole.GuardMode) (*cache.RedisCacheAdapter, *cache.EvictableRedisAdapter) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	guard := redisrole.NewGuard(testenv.NewLogger(t), testenv.NewMeterProvider(t), mode)
	return cache.NewRedisCacheAdapter(client), cache.NewEvictableRedisAdapter(redisrole.EvictableClient{Client: client}, guard)
}

// evictableSampleKeys is one sample key for every declared evictable prefix.
func evictableSampleKeys() []string {
	prefixes := redisrole.EvictablePrefixes()
	keys := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		keys = append(keys, prefix+"sample:")
	}
	return keys
}

func TestEvictableAdapterSharesDataWithSessionsAdapterOnOneClient(t *testing.T) {
	t.Parallel()

	sessions, evictable := newEvictablePair(t, redisrole.GuardEnforce)
	ctx := t.Context()

	for _, key := range evictableSampleKeys() {
		require.NoError(t, evictable.Set(ctx, key, "from-evictable", time.Minute), key)
		var got string
		require.NoError(t, sessions.Get(ctx, key, &got), key)
		require.Equal(t, "from-evictable", got)

		require.NoError(t, sessions.Set(ctx, key, "from-sessions", time.Minute), key)
		require.NoError(t, evictable.Get(ctx, key, &got), key)
		require.Equal(t, "from-sessions", got)

		require.NoError(t, evictable.Delete(ctx, key), key)
		require.Error(t, sessions.Get(ctx, key, &got), key)
	}
}

// evictableOperations calls every adapter method with key and returns each
// error by method name.
func evictableOperations(ctx context.Context, adapter *cache.EvictableRedisAdapter, key string) map[string]error {
	errs := map[string]error{}
	var value string
	errs["Set"] = adapter.Set(ctx, key, "v", time.Minute)
	errs["Get"] = adapter.Get(ctx, key, &value)
	errs["Update"] = adapter.Update(ctx, key, "v2")
	errs["Expire"] = adapter.Expire(ctx, key, time.Minute)
	_, errs["CompareAndSwap"] = adapter.CompareAndSwap(ctx, key, "v2", "v3", time.Minute)
	_, errs["CompareAndDelete"] = adapter.CompareAndDelete(ctx, key, "v3")
	_, errs["SetIfAbsent"] = adapter.SetIfAbsent(ctx, key, "v", time.Minute)
	errs["GetAndDelete"] = adapter.GetAndDelete(ctx, key, &value)
	_, errs["Add"] = adapter.Add(ctx, key, time.Minute)
	errs["Delete"] = adapter.Delete(ctx, key)
	_, errs["AcquireLease"] = adapter.AcquireLease(ctx, key, "owner", time.Minute)
	_, errs["RenewLease"] = adapter.RenewLease(ctx, key, "owner", time.Minute)
	_, errs["ReleaseLeaseIfOwner"] = adapter.ReleaseLeaseIfOwner(ctx, key, "owner")
	_, errs["ClaimLease"] = adapter.ClaimLease(ctx, key, "owner", time.Minute)
	errs["ReleaseLease"] = adapter.ReleaseLease(ctx, key, "owner")
	errs["Mutate"] = adapter.Mutate(ctx, key, &value, time.Minute, func(bool) error { value = "m"; return nil })
	errs["Delete2"] = adapter.Delete(ctx, key)
	errs["ListAppend"] = adapter.ListAppend(ctx, key+"list", map[string]string{"item": "value"}, time.Minute)
	var items []map[string]string
	errs["ListRange"] = adapter.ListRange(ctx, key+"list", 0, -1, &items)
	_, errs["ScanKeys"] = adapter.ScanKeys(ctx, key)
	errs["DeleteByPrefix"] = adapter.DeleteByPrefix(ctx, key)
	return errs
}

func TestEvictableAdapterAllowsEvictableKeysInEnforce(t *testing.T) {
	t.Parallel()

	_, evictable := newEvictablePair(t, redisrole.GuardEnforce)
	for _, key := range evictableSampleKeys() {
		for method, err := range evictableOperations(t.Context(), evictable, key) {
			require.NoError(t, err, "%s on %q", method, key)
		}
	}
}

func TestEvictableAdapterRefusesSessionsKeysInEnforce(t *testing.T) {
	t.Parallel()

	_, evictable := newEvictablePair(t, redisrole.GuardEnforce)
	for method, err := range evictableOperations(t.Context(), evictable, "sessions:v2:sid:") {
		require.ErrorIs(t, err, redisrole.ErrWrongRole, method)
	}

	var value string
	err := evictable.GetAndDeleteMarking(t.Context(), "toolset:a:", "connectionAttemptEnded:a", time.Minute, &value)
	require.ErrorIs(t, err, redisrole.ErrWrongRole)
}

func TestEvictableAdapterLetsSessionsKeysThroughInReport(t *testing.T) {
	t.Parallel()

	sessions, evictable := newEvictablePair(t, redisrole.GuardReport)
	require.NoError(t, evictable.Set(t.Context(), "sessions:v2:sid:", "v", time.Minute))

	var got string
	require.NoError(t, sessions.Get(t.Context(), "sessions:v2:sid:", &got))
	require.Equal(t, "v", got)
}

func TestEvictableAdapterSupportsOptionalInterfaces(t *testing.T) {
	t.Parallel()

	_, evictable := newEvictablePair(t, redisrole.GuardEnforce)
	var store cache.Cache = evictable

	_, ok := store.(cache.LeaseCache)
	require.True(t, ok)
	_, ok = store.(cache.CompareAndSwapCache)
	require.True(t, ok)
	_, ok = store.(cache.ConditionalCache)
	require.True(t, ok)
	_, ok = store.(cache.MarkingConsumeCache)
	require.True(t, ok)
}

package cache_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisCache "github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type conditionalTestObject struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (o conditionalTestObject) CacheKey() string   { return o.Key }
func (o conditionalTestObject) TTL() time.Duration { return time.Minute }

func TestRedisLeaseReleaseRequiresOwner(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	adapter := cache.NewRedisCacheAdapter(client)

	acquired, err := adapter.AcquireLease(t.Context(), "lease", "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	released, err := adapter.ReleaseLeaseIfOwner(t.Context(), "lease", "owner-b")
	require.NoError(t, err)
	require.False(t, released)
	require.True(t, mr.Exists("lease"))
	released, err = adapter.ReleaseLeaseIfOwner(t.Context(), "lease", "owner-a")
	require.NoError(t, err)
	require.True(t, released)
	require.False(t, mr.Exists("lease"))
}

func TestTypedCacheObjectStoreIfAbsentPreservesFirstValue(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	objects := cache.NewTypedObjectCache[conditionalTestObject](testenv.NewLogger(t), cache.NewRedisCacheAdapter(client), cache.SuffixNone)

	stored, err := objects.StoreIfAbsent(t.Context(), conditionalTestObject{Key: "outcome", Value: "success"})
	require.NoError(t, err)
	require.True(t, stored)
	stored, err = objects.StoreIfAbsent(t.Context(), conditionalTestObject{Key: "outcome", Value: "failure"})
	require.NoError(t, err)
	require.False(t, stored)

	got, err := objects.Get(t.Context(), "outcome")
	require.NoError(t, err)
	require.Equal(t, "success", got.Value)
}

func TestTypedCacheObjectGetAndDeleteMarking(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	adapter := cache.NewRedisCacheAdapter(client)
	objects := cache.NewTypedObjectCache[conditionalTestObject](testenv.NewLogger(t), adapter, cache.SuffixNone)

	_, err := objects.GetAndDeleteMarking(t.Context(), "missing", "marker-missing", time.Minute)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss)
	require.False(t, mr.Exists("marker-missing"), "nothing was consumed, so nothing is reserved")

	require.NoError(t, objects.Store(t.Context(), conditionalTestObject{Key: "key", Value: "value"}))
	got, err := objects.GetAndDeleteMarking(t.Context(), "key", "marker", time.Minute)
	require.NoError(t, err)
	require.Equal(t, "value", got.Value)

	_, err = objects.Get(t.Context(), "key")
	require.Error(t, err, "consumed")
	won, err := adapter.Add(t.Context(), "marker", time.Minute)
	require.NoError(t, err)
	require.False(t, won, "the marker was set with the consume")
}

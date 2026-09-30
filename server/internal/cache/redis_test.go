package cache_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
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

func TestTypedCacheCompareAndSwapPreservesExpiry(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	objects := cache.NewTypedObjectCache[conditionalTestObject](testenv.NewLogger(t), cache.NewRedisCacheAdapter(client), cache.SuffixNone)
	original := conditionalTestObject{Key: "challenge", Value: "unconfirmed"}
	marked := conditionalTestObject{Key: "challenge", Value: "confirmation-required"}
	require.NoError(t, objects.Store(t.Context(), original))
	mr.FastForward(45 * time.Second)
	remaining := mr.TTL("challenge:")
	require.Equal(t, 15*time.Second, remaining)
	swapped, err := objects.CompareAndSwapPreservingTTL(t.Context(), original, marked)
	require.NoError(t, err)
	require.True(t, swapped)
	require.Equal(t, remaining, mr.TTL("challenge:"), "marking must not restart the challenge lifetime")
	got, err := objects.Get(t.Context(), original.Key)
	require.NoError(t, err)
	require.Equal(t, marked, got)
	swapped, err = objects.CompareAndSwapPreservingTTL(t.Context(), original, marked)
	require.NoError(t, err)
	require.False(t, swapped, "stale writers must not overwrite changed state")
	mr.FastForward(16 * time.Second)
	swapped, err = objects.CompareAndSwapPreservingTTL(t.Context(), marked, original)
	require.NoError(t, err)
	require.False(t, swapped, "expired challenge must not be revived")
	require.False(t, mr.Exists("challenge:"))
	require.NoError(t, objects.Store(t.Context(), original))
	_, err = objects.GetAndDelete(t.Context(), original.Key)
	require.NoError(t, err)
	swapped, err = objects.CompareAndSwapPreservingTTL(t.Context(), original, marked)
	require.NoError(t, err)
	require.False(t, swapped, "consumed challenge must not be revived")
	require.False(t, mr.Exists("challenge:"))
}

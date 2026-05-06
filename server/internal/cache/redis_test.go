package cache_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"
)

type cacheItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestRedis_SetAndGet_RoundTrip(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()
	key := "round-trip-" + t.Name()

	require.NoError(t, c.Set(ctx, key, cacheItem{Name: "alice", Count: 3}, time.Minute))

	var got cacheItem
	require.NoError(t, c.Get(ctx, key, &got))
	require.Equal(t, "alice", got.Name)
	require.Equal(t, 3, got.Count)
}

func TestRedis_Get_MissingKeyReturnsCacheMiss(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()

	var got cacheItem
	err := c.Get(ctx, "missing-"+t.Name(), &got)
	require.Error(t, err)
	require.True(t, errors.Is(err, redisCache.ErrCacheMiss), "expected ErrCacheMiss, got %v", err)
}

func TestRedis_Set_OverwritesValue(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()
	key := "overwrite-" + t.Name()

	require.NoError(t, c.Set(ctx, key, cacheItem{Name: "first"}, time.Minute))
	require.NoError(t, c.Set(ctx, key, cacheItem{Name: "second"}, time.Minute))

	var got cacheItem
	require.NoError(t, c.Get(ctx, key, &got))
	require.Equal(t, "second", got.Name)
}

func TestRedis_Delete_RemovesValue(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()
	key := "delete-" + t.Name()

	require.NoError(t, c.Set(ctx, key, cacheItem{Name: "x"}, time.Minute))
	require.NoError(t, c.Delete(ctx, key))

	var got cacheItem
	err := c.Get(ctx, key, &got)
	require.Error(t, err)
	require.True(t, errors.Is(err, redisCache.ErrCacheMiss))
}

func TestRedis_Delete_MissingKey_NoError(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()

	require.NoError(t, c.Delete(ctx, "never-set-"+t.Name()))
}

func TestRedis_Update_PreservesTTL(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()
	key := "update-ttl-" + t.Name()

	require.NoError(t, c.Set(ctx, key, cacheItem{Name: "v1"}, time.Hour))
	require.NoError(t, c.Update(ctx, key, cacheItem{Name: "v2"}))

	var got cacheItem
	require.NoError(t, c.Get(ctx, key, &got))
	require.Equal(t, "v2", got.Name)
}

func TestRedis_Update_MissingKey_Errors(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()

	err := c.Update(ctx, "missing-update-"+t.Name(), cacheItem{Name: "x"})
	require.Error(t, err)
}

func TestRedis_Set_WithTTL_Expires(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()
	key := "ttl-expiry-" + t.Name()

	require.NoError(t, c.Set(ctx, key, cacheItem{Name: "ephemeral"}, 100*time.Millisecond))

	// Wait past the TTL.
	require.Eventually(t, func() bool {
		var got cacheItem
		err := c.Get(ctx, key, &got)
		return errors.Is(err, redisCache.ErrCacheMiss)
	}, 2*time.Second, 50*time.Millisecond, "key should expire")
}

func TestRedis_ListAppend_ListRange(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()
	key := "list-append-" + t.Name()

	for i := 0; i < 3; i++ {
		require.NoError(t, c.ListAppend(ctx, key, cacheItem{Name: "n" + strconv.Itoa(i), Count: i}, time.Minute))
	}

	var got []cacheItem
	require.NoError(t, c.ListRange(ctx, key, 0, -1, &got))
	require.Len(t, got, 3)
	require.Equal(t, "n0", got[0].Name)
	require.Equal(t, "n2", got[2].Name)
	require.Equal(t, 0, got[0].Count)
	require.Equal(t, 2, got[2].Count)
}

func TestRedis_ListRange_EmptyList(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()

	var got []cacheItem
	require.NoError(t, c.ListRange(ctx, "no-list-"+t.Name(), 0, -1, &got))
	require.Empty(t, got)
}

func TestRedis_ListRange_PartialSlice(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()
	key := "list-partial-" + t.Name()

	for i := 0; i < 5; i++ {
		require.NoError(t, c.ListAppend(ctx, key, cacheItem{Count: i}, time.Minute))
	}

	var got []cacheItem
	require.NoError(t, c.ListRange(ctx, key, 1, 3, &got))
	require.Len(t, got, 3)
	require.Equal(t, 1, got[0].Count)
	require.Equal(t, 3, got[2].Count)
}

func TestRedis_DeleteByPrefix(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()
	prefix := "prefix-" + t.Name() + ":"

	require.NoError(t, c.Set(ctx, prefix+"a", cacheItem{Name: "a"}, time.Minute))
	require.NoError(t, c.Set(ctx, prefix+"b", cacheItem{Name: "b"}, time.Minute))
	require.NoError(t, c.Set(ctx, "other-prefix:c", cacheItem{Name: "c"}, time.Minute))

	require.NoError(t, c.DeleteByPrefix(ctx, prefix))

	var got cacheItem
	require.True(t, errors.Is(c.Get(ctx, prefix+"a", &got), redisCache.ErrCacheMiss))
	require.True(t, errors.Is(c.Get(ctx, prefix+"b", &got), redisCache.ErrCacheMiss))

	// Keys outside the prefix must remain.
	require.NoError(t, c.Get(ctx, "other-prefix:c", &got))
	require.Equal(t, "c", got.Name)
}

func TestRedis_DeleteByPrefix_NoMatches(t *testing.T) {
	t.Parallel()

	c := newRedisCache(t)
	ctx := t.Context()

	require.NoError(t, c.DeleteByPrefix(ctx, "no-such-prefix-"+t.Name()))
}

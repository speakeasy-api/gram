package cache_test

import (
	"errors"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type widget struct {
	Key      string
	Aliases  []string
	Lifetime time.Duration
	Payload  string
}

func (w widget) CacheKey() string              { return w.Key }
func (w widget) AdditionalCacheKeys() []string { return w.Aliases }
func (w widget) TTL() time.Duration {
	if w.Lifetime == 0 {
		return time.Minute
	}
	return w.Lifetime
}

func newWidgetCache(t *testing.T, suffix cache.Suffix) cache.TypedCacheObject[widget] {
	t.Helper()
	return cache.NewTypedObjectCache[widget](testenv.NewLogger(t), newRedisCache(t), suffix)
}

func TestTyped_StoreAndGet(t *testing.T) {
	t.Parallel()

	c := newWidgetCache(t, cache.Suffix("widget"))
	ctx := t.Context()

	w := widget{Key: "store-and-get-" + t.Name(), Payload: "hello"}
	require.NoError(t, c.Store(ctx, w))

	got, err := c.Get(ctx, w.Key)
	require.NoError(t, err)
	require.Equal(t, "hello", got.Payload)
}

func TestTyped_Get_KeyIncludesSuffix(t *testing.T) {
	t.Parallel()

	suffix := cache.Suffix("widget-suffix-" + t.Name())
	c := newWidgetCache(t, suffix)
	ctx := t.Context()

	// Store via typed cache
	w := widget{Key: "k1", Payload: "value-with-suffix"}
	require.NoError(t, c.Store(ctx, w))

	// Reading the same logical key under a different suffix must miss.
	other := newWidgetCache(t, cache.Suffix("other"))
	_, err := other.Get(ctx, "k1")
	require.Error(t, err)
}

func TestTyped_Get_MissingReturnsError(t *testing.T) {
	t.Parallel()

	c := newWidgetCache(t, cache.SuffixNone)
	_, err := c.Get(t.Context(), "missing-"+t.Name())
	require.Error(t, err)
	require.True(t, errors.Is(err, redisCache.ErrCacheMiss))
}

func TestTyped_Store_AdditionalKeys(t *testing.T) {
	t.Parallel()

	c := newWidgetCache(t, cache.SuffixNone)
	ctx := t.Context()

	w := widget{
		Key:     "primary-" + t.Name(),
		Aliases: []string{"alias-1-" + t.Name(), "alias-2-" + t.Name()},
		Payload: "shared",
	}
	require.NoError(t, c.Store(ctx, w))

	got, err := c.Get(ctx, w.Aliases[0])
	require.NoError(t, err)
	require.Equal(t, "shared", got.Payload)

	got, err = c.Get(ctx, w.Aliases[1])
	require.NoError(t, err)
	require.Equal(t, "shared", got.Payload)
}

func TestTyped_Delete_RemovesPrimaryAndAliases(t *testing.T) {
	t.Parallel()

	c := newWidgetCache(t, cache.SuffixNone)
	ctx := t.Context()

	w := widget{
		Key:     "to-delete-" + t.Name(),
		Aliases: []string{"to-delete-alias-" + t.Name()},
		Payload: "x",
	}
	require.NoError(t, c.Store(ctx, w))
	require.NoError(t, c.Delete(ctx, w))

	_, err := c.Get(ctx, w.Key)
	require.Error(t, err)
	_, err = c.Get(ctx, w.Aliases[0])
	require.Error(t, err)
}

func TestTyped_DeleteByKey(t *testing.T) {
	t.Parallel()

	c := newWidgetCache(t, cache.SuffixNone)
	ctx := t.Context()

	w := widget{Key: "by-key-" + t.Name(), Payload: "x"}
	require.NoError(t, c.Store(ctx, w))
	require.NoError(t, c.DeleteByKey(ctx, w.Key))

	_, err := c.Get(ctx, w.Key)
	require.Error(t, err)
}

func TestTyped_DeleteByPrefix(t *testing.T) {
	t.Parallel()

	suffix := cache.Suffix("dp-" + t.Name())
	c := newWidgetCache(t, suffix)
	ctx := t.Context()

	require.NoError(t, c.Store(ctx, widget{Key: "p1", Payload: "a"}))
	require.NoError(t, c.Store(ctx, widget{Key: "p2", Payload: "b"}))

	// DeleteByPrefix must scope to the typed-cache suffix.
	require.NoError(t, c.DeleteByPrefix(ctx, "p"))

	_, err := c.Get(ctx, "p1")
	require.Error(t, err)
	_, err = c.Get(ctx, "p2")
	require.Error(t, err)
}

func TestTyped_Update_RequiresExistingKey(t *testing.T) {
	t.Parallel()

	c := newWidgetCache(t, cache.SuffixNone)

	err := c.Update(t.Context(), widget{Key: "missing-" + t.Name(), Payload: "x"})
	require.Error(t, err)
}

func TestTyped_Update_PreservesTTLAndUpdatesValue(t *testing.T) {
	t.Parallel()

	c := newWidgetCache(t, cache.SuffixNone)
	ctx := t.Context()

	w := widget{Key: "upd-" + t.Name(), Payload: "v1", Lifetime: time.Hour}
	require.NoError(t, c.Store(ctx, w))

	w.Payload = "v2"
	require.NoError(t, c.Update(ctx, w))

	got, err := c.Get(ctx, w.Key)
	require.NoError(t, err)
	require.Equal(t, "v2", got.Payload)
}

func TestTyped_SkipCache_StoreIsNoop(t *testing.T) {
	t.Parallel()

	base := newWidgetCache(t, cache.SuffixNone)
	c := base.SkipCache()
	ctx := t.Context()

	w := widget{Key: "skip-" + t.Name(), Payload: "should-not-persist"}
	require.NoError(t, c.Store(ctx, w))

	// SkipCache uses NoopCache, which always errors on Get.
	_, err := c.Get(ctx, w.Key)
	require.Error(t, err)
}

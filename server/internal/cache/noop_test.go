package cache_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

func TestNoopCache_GetAlwaysErrors(t *testing.T) {
	t.Parallel()

	var v string
	err := cache.NoopCache.Get(t.Context(), "any-key", &v)
	require.Error(t, err)
	require.Equal(t, "", v)
}

func TestNoopCache_SetIsNoop(t *testing.T) {
	t.Parallel()

	require.NoError(t, cache.NoopCache.Set(t.Context(), "k", "v", time.Minute))

	var v string
	err := cache.NoopCache.Get(t.Context(), "k", &v)
	require.Error(t, err, "noop must not actually persist")
}

func TestNoopCache_Update(t *testing.T) {
	t.Parallel()

	require.NoError(t, cache.NoopCache.Update(t.Context(), "k", "v"))
}

func TestNoopCache_Delete(t *testing.T) {
	t.Parallel()

	require.NoError(t, cache.NoopCache.Delete(t.Context(), "k"))
}

func TestNoopCache_DeleteByPrefix(t *testing.T) {
	t.Parallel()

	require.NoError(t, cache.NoopCache.DeleteByPrefix(t.Context(), "prefix"))
}

func TestNoopCache_ListAppend(t *testing.T) {
	t.Parallel()

	require.NoError(t, cache.NoopCache.ListAppend(t.Context(), "k", "v", time.Minute))
}

func TestNoopCache_ListRange(t *testing.T) {
	t.Parallel()

	var out []string
	require.NoError(t, cache.NoopCache.ListRange(t.Context(), "k", 0, -1, &out))
	require.Empty(t, out)
}

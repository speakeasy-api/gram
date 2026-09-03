package growthsignals

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The point of the cache is that a burst of events from one organization costs
// one query, so a repeat key must not reach the loader again.
func TestLookupCacheServesRepeatsFromMemory(t *testing.T) {
	t.Parallel()

	var loads atomic.Int64
	cache := newLookupCache(func(_ context.Context, key string) (string, error) {
		loads.Add(1)
		return "resolved " + key, nil
	})

	for range 5 {
		value, err := cache.resolve(t.Context(), "org_placeholder")

		require.NoError(t, err)
		require.Equal(t, "resolved org_placeholder", value)
	}

	require.Equal(t, int64(1), loads.Load())
}

func TestLookupCacheKeepsKeysApart(t *testing.T) {
	t.Parallel()

	var loads atomic.Int64
	cache := newLookupCache(func(_ context.Context, key string) (string, error) {
		loads.Add(1)
		return "resolved " + key, nil
	})

	tests := []string{"org_first", "org_second", "org_first", "org_second"}
	for _, key := range tests {
		value, err := cache.resolve(t.Context(), key)

		require.NoError(t, err)
		require.Equal(t, "resolved "+key, value, "value for %s", key)
	}

	require.Equal(t, int64(2), loads.Load())
}

// An id that resolves to nothing is the common case on a firehose, so the empty
// answer has to be as cheap the second time as a found one.
func TestLookupCacheRemembersEmptyResults(t *testing.T) {
	t.Parallel()

	var loads atomic.Int64
	cache := newLookupCache(func(_ context.Context, _ string) (string, error) {
		loads.Add(1)
		return "", nil
	})

	for range 3 {
		value, err := cache.resolve(t.Context(), "org_deleted")

		require.NoError(t, err)
		require.Empty(t, value)
	}

	require.Equal(t, int64(1), loads.Load())
}

// Caching a failure would turn a database blip into a TTL of blank events.
func TestLookupCacheDoesNotRememberFailures(t *testing.T) {
	t.Parallel()

	var loads atomic.Int64
	failure := errors.New("lookup unavailable")
	cache := newLookupCache(func(_ context.Context, key string) (string, error) {
		if loads.Add(1) == 1 {
			return "", failure
		}

		return "resolved " + key, nil
	})

	_, err := cache.resolve(t.Context(), "org_placeholder")
	require.ErrorIs(t, err, failure)

	value, err := cache.resolve(t.Context(), "org_placeholder")
	require.NoError(t, err)
	require.Equal(t, "resolved org_placeholder", value)
	require.Equal(t, int64(2), loads.Load())
}

// A renamed organization has to catch up, so entries must not be permanent.
func TestLookupCacheExpiresEntries(t *testing.T) {
	t.Parallel()

	var loads atomic.Int64
	cache := &lookupCache[string, string]{
		entries: expirable.NewLRU[string, string](lookupCacheSize, nil, 10*time.Millisecond),
		load: func(_ context.Context, _ string) (string, error) {
			loads.Add(1)
			return "resolved", nil
		},
	}

	_, err := cache.resolve(t.Context(), "org_placeholder")
	require.NoError(t, err)
	require.Equal(t, int64(1), loads.Load())

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		_, err := cache.resolve(t.Context(), "org_placeholder")
		assert.NoError(collect, err)
		assert.Greater(collect, loads.Load(), int64(1))
	}, 5*time.Second, 5*time.Millisecond)
}

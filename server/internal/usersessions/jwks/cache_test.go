package jwks

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryCache_PutGet(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	state := CacheState{
		Document:    json.RawMessage(`{"keys":[]}`),
		ETag:        `"v1"`,
		ExpiresAt:   time.Now().Add(time.Hour),
		RefreshedAt: time.Now(),
	}
	require.NoError(t, cache.Put(t.Context(), "https://example.com/jwks.json", state))

	got, err := cache.Get(t.Context(), "https://example.com/jwks.json")
	require.NoError(t, err)
	require.Equal(t, state, got)

	got, err = cache.Get(t.Context(), "https://example.com/other.json")
	require.NoError(t, err)
	require.Zero(t, got, "a miss is the zero CacheState")
}

func TestMemoryCache_ExpiredEntriesStillReturned(t *testing.T) {
	t.Parallel()

	// Freshness is the resolver's decision: an expired entry still carries
	// the document and validator a conditional refresh needs.
	cache := NewMemoryCache()
	state := CacheState{
		Document:    json.RawMessage(`{"keys":[]}`),
		ETag:        `"v1"`,
		ExpiresAt:   time.Now().Add(-time.Hour),
		RefreshedAt: time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, cache.Put(t.Context(), "k", state))

	got, err := cache.Get(t.Context(), "k")
	require.NoError(t, err)
	require.Equal(t, state, got)
}

func TestMemoryCache_EvictsEarliestExpiryAtCapacity(t *testing.T) {
	t.Parallel()

	cache := &MemoryCache{mu: sync.RWMutex{}, entries: make(map[string]CacheState), maxEntries: 2, maxBytes: defaultMemoryCacheBytes, totalBytes: 0}
	older := CacheState{Document: nil, ETag: "", ExpiresAt: time.Now().Add(time.Minute), RefreshedAt: time.Now()}
	newer := CacheState{Document: nil, ETag: "", ExpiresAt: time.Now().Add(time.Hour), RefreshedAt: time.Now()}
	require.NoError(t, cache.Put(t.Context(), "older", older))
	require.NoError(t, cache.Put(t.Context(), "newer", newer))

	require.NoError(t, cache.Put(t.Context(), "third", newer))

	got, err := cache.Get(t.Context(), "older")
	require.NoError(t, err)
	require.Zero(t, got, "the entry closest to expiry is the one evicted")

	got, err = cache.Get(t.Context(), "newer")
	require.NoError(t, err)
	require.NotZero(t, got)

	got, err = cache.Get(t.Context(), "third")
	require.NoError(t, err)
	require.NotZero(t, got)
}

func TestMemoryCache_EvictsOverByteBudget(t *testing.T) {
	t.Parallel()

	// The entry cap alone would admit maxEntries maximum-size documents;
	// the byte budget evicts earliest-expiry entries until the sum fits.
	cache := &MemoryCache{mu: sync.RWMutex{}, entries: make(map[string]CacheState), maxEntries: 1024, maxBytes: 1024, totalBytes: 0}
	bigDoc := json.RawMessage(bytes.Repeat([]byte("a"), 600))
	older := CacheState{Document: bigDoc, ETag: "", ExpiresAt: time.Now().Add(time.Minute), RefreshedAt: time.Now()}
	newer := CacheState{Document: bigDoc, ETag: "", ExpiresAt: time.Now().Add(time.Hour), RefreshedAt: time.Now()}
	require.NoError(t, cache.Put(t.Context(), "older", older))

	require.NoError(t, cache.Put(t.Context(), "newer", newer))

	got, err := cache.Get(t.Context(), "older")
	require.NoError(t, err)
	require.Zero(t, got, "the byte budget evicts the entry closest to expiry")

	got, err = cache.Get(t.Context(), "newer")
	require.NoError(t, err)
	require.NotZero(t, got, "the just-written entry always lands")
}

func TestMemoryCache_ReplacementAdjustsByteBudget(t *testing.T) {
	t.Parallel()

	// Re-putting a key must not double-count its bytes: two replacements of
	// one 600-byte document under a 1024-byte budget stay in budget, so a
	// second small entry survives.
	cache := &MemoryCache{mu: sync.RWMutex{}, entries: make(map[string]CacheState), maxEntries: 1024, maxBytes: 1024, totalBytes: 0}
	bigDoc := json.RawMessage(bytes.Repeat([]byte("a"), 600))
	small := CacheState{Document: json.RawMessage(`{"keys":[]}`), ETag: "", ExpiresAt: time.Now().Add(time.Hour), RefreshedAt: time.Now()}
	big := CacheState{Document: bigDoc, ETag: "", ExpiresAt: time.Now().Add(time.Hour), RefreshedAt: time.Now()}
	require.NoError(t, cache.Put(t.Context(), "small", small))
	require.NoError(t, cache.Put(t.Context(), "big", big))
	require.NoError(t, cache.Put(t.Context(), "big", big))

	got, err := cache.Get(t.Context(), "small")
	require.NoError(t, err)
	require.NotZero(t, got)
}

func TestMemoryCache_ReplacingExistingKeyDoesNotEvict(t *testing.T) {
	t.Parallel()

	cache := &MemoryCache{mu: sync.RWMutex{}, entries: make(map[string]CacheState), maxEntries: 2, maxBytes: defaultMemoryCacheBytes, totalBytes: 0}
	state := CacheState{Document: nil, ETag: "", ExpiresAt: time.Now().Add(time.Hour), RefreshedAt: time.Now()}
	require.NoError(t, cache.Put(t.Context(), "a", state))
	require.NoError(t, cache.Put(t.Context(), "b", state))

	require.NoError(t, cache.Put(t.Context(), "a", state))

	got, err := cache.Get(t.Context(), "b")
	require.NoError(t, err)
	require.NotZero(t, got)
}

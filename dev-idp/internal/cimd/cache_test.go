package cimd

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/jwks"
)

func TestExpiredCacheEntriesAreDeleted(t *testing.T) {
	t.Parallel()

	r := NewResolver(http.DefaultClient, time.Minute)
	r.cache["expired"] = cacheEntry{doc: zeroDocument, expiresAt: time.Now().Add(-time.Second)}
	r.keyCache["expired"] = keyEntry{
		keys:      jwks.Document{Keys: nil},
		hasKeys:   false,
		expiresAt: time.Now().Add(-time.Second),
	}

	_, ok := r.cached("expired")
	require.False(t, ok)
	_, ok = r.cachedKeys("expired")
	require.False(t, ok)
	require.NotContains(t, r.cache, "expired")
	require.NotContains(t, r.keyCache, "expired")
}

func TestPruneExpiredClients(t *testing.T) {
	t.Parallel()

	r := NewResolver(http.DefaultClient, time.Minute)
	r.cache["expired-document"] = cacheEntry{doc: zeroDocument, expiresAt: time.Now().Add(-time.Second)}
	r.keyCache["expired-keys"] = keyEntry{
		keys:      jwks.Document{Keys: nil},
		hasKeys:   false,
		expiresAt: time.Now().Add(-time.Second),
	}
	r.cache["active"] = cacheEntry{doc: zeroDocument, expiresAt: time.Now().Add(time.Minute)}

	r.mu.Lock()
	r.pruneExpiredLocked(time.Now())
	r.mu.Unlock()

	require.NotContains(t, r.cache, "expired-document")
	require.NotContains(t, r.keyCache, "expired-keys")
	require.Contains(t, r.cache, "active")
}

package idjag

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

func TestIssuerVerificationKeysUseKnownStaleKeyOnTransientFailure(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	source, err := jwks.NewRemoteSource("https://idp.example.com/jwks")
	require.NoError(t, err)
	source = source.WithCacheKey(issuerCacheKey(uuid.New(), "https://idp.example.com/jwks"))
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{*assertion.key}})
	require.NoError(t, err)
	cache := jwks.NewMemoryCache()
	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), jwks.CacheState{
		Document: document, ETag: "", ExpiresAt: time.Now().Add(-time.Minute), RefreshedAt: time.Now().Add(-2 * time.Minute),
	}))
	base := &testKeys{key: nil, err: jwks.ErrKeySetUnavailable, calls: 0}
	keys, err := NewIssuerVerificationKeys(base, cache)
	require.NoError(t, err)
	key, err := keys.VerificationKey(t.Context(), source, "key-1")
	require.NoError(t, err)
	require.Equal(t, "key-1", key.KeyID)
	_, err = keys.VerificationKey(t.Context(), source, "unknown")
	require.ErrorIs(t, err, jwks.ErrKeySetUnavailable)
	base.err = guardian.ErrBlockedIP
	_, err = keys.VerificationKey(t.Context(), source, "key-1")
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestIssuerVerificationKeysRejectPastStaleBound(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	source, err := jwks.NewRemoteSource("https://idp.example.com/jwks")
	require.NoError(t, err)
	source = source.WithCacheKey(issuerCacheKey(uuid.New(), "https://idp.example.com/jwks"))
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{*assertion.key}})
	require.NoError(t, err)
	cache := jwks.NewMemoryCache()
	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), jwks.CacheState{
		Document: document, ETag: "", ExpiresAt: time.Now().Add(-MaxStaleKeys - time.Minute), RefreshedAt: time.Now().Add(-MaxStaleKeys - 2*time.Minute),
	}))
	base := &testKeys{key: nil, err: jwks.ErrKeySetUnavailable, calls: 0}
	keys, err := NewIssuerVerificationKeys(base, cache)
	require.NoError(t, err)
	_, err = keys.VerificationKey(t.Context(), source, "key-1")
	require.ErrorIs(t, err, jwks.ErrKeySetUnavailable)
}

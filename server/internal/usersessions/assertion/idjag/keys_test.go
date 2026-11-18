package idjag

import (
	"context"
	"encoding/json"
	"errors"
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
	source = source.WithCacheKey(issuerCacheKey("test-org", uuid.New(), testIssuer, "https://idp.example.com/jwks"))
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{*assertion.key}})
	require.NoError(t, err)
	cache := jwks.NewMemoryCache()
	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), jwks.CacheState{
		Document: document, ETag: "", ExpiresAt: time.Now().Add(-time.Minute), RefreshedAt: time.Now().Add(-2 * time.Minute),
		LastErrorAt: time.Time{},
		LastError:   "",
		Revision:    "",
	}))
	base := &testKeys{key: nil, err: jwks.ErrKeySetUnavailable, calls: 0}
	keys, err := NewIssuerVerificationKeys(base, cache)
	require.NoError(t, err)
	key, err := keys.VerificationKeyForAlgorithm(t.Context(), source, "key-1", jose.ES256)
	require.NoError(t, err)
	require.Equal(t, "key-1", key.KeyID)
	_, err = keys.VerificationKeyForAlgorithm(t.Context(), source, "unknown", jose.ES256)
	require.ErrorIs(t, err, jwks.ErrKeySetUnavailable)
	base.err = guardian.ErrBlockedIP
	_, err = keys.VerificationKeyForAlgorithm(t.Context(), source, "key-1", jose.ES256)
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestIssuerVerificationKeysRejectPastStaleBound(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	source, err := jwks.NewRemoteSource("https://idp.example.com/jwks")
	require.NoError(t, err)
	source = source.WithCacheKey(issuerCacheKey("test-org", uuid.New(), testIssuer, "https://idp.example.com/jwks"))
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{*assertion.key}})
	require.NoError(t, err)
	cache := jwks.NewMemoryCache()
	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), jwks.CacheState{
		Document: document, ETag: "", ExpiresAt: time.Now().Add(-MaxStaleKeys - time.Minute), RefreshedAt: time.Now().Add(-MaxStaleKeys - 2*time.Minute),
		LastErrorAt: time.Time{},
		LastError:   "",
		Revision:    "",
	}))
	base := &testKeys{key: nil, err: jwks.ErrKeySetUnavailable, calls: 0}
	keys, err := NewIssuerVerificationKeys(base, cache)
	require.NoError(t, err)
	_, err = keys.VerificationKeyForAlgorithm(t.Context(), source, "key-1", jose.ES256)
	require.ErrorIs(t, err, jwks.ErrKeySetUnavailable)
}

func TestIssuerVerificationKeysUsesFreshStateAfterConcurrentRefresh(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	source, err := jwks.NewRemoteSource("https://idp.example.com/jwks")
	require.NoError(t, err)
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{*assertion.key}})
	require.NoError(t, err)
	cache := jwks.NewMemoryCache()
	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), jwks.CacheState{
		Document: document, ETag: "", ExpiresAt: time.Now().Add(time.Hour), RefreshedAt: time.Now(),
		LastErrorAt: time.Time{}, LastError: "", Revision: "",
	}))
	keys, err := NewIssuerVerificationKeys(&testKeys{key: nil, err: jwks.ErrKeySetUnavailable, calls: 0}, cache)
	require.NoError(t, err)
	key, err := keys.VerificationKeyForAlgorithm(t.Context(), source, "key-1", jose.ES256)
	require.NoError(t, err)
	require.Equal(t, "key-1", key.KeyID)
}

func TestIssuerVerificationKeysSelectsStaleKeyByAlgorithm(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	source, err := jwks.NewRemoteSource("https://idp.example.com/jwks")
	require.NoError(t, err)
	otherAlgorithm := *assertion.key
	otherAlgorithm.Algorithm = string(jose.ES384)
	document, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{otherAlgorithm, *assertion.key}})
	require.NoError(t, err)
	cache := jwks.NewMemoryCache()
	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), jwks.CacheState{
		Document: document, ETag: "", ExpiresAt: time.Now().Add(-time.Minute), RefreshedAt: time.Now().Add(-time.Hour),
		LastErrorAt: time.Time{}, LastError: "", Revision: "",
	}))
	keys, err := NewIssuerVerificationKeys(&testKeys{key: nil, err: jwks.ErrKeySetUnavailable, calls: 0}, cache)
	require.NoError(t, err)
	key, err := keys.VerificationKeyForAlgorithm(t.Context(), source, "key-1", jose.ES256)
	require.NoError(t, err)
	require.Equal(t, string(jose.ES256), key.Algorithm)
}

// failingCache is a key store that cannot be read.
type failingCache struct{ jwks.Cache }

func (failingCache) Get(context.Context, string) (jwks.CacheState, error) {
	return jwks.CacheState{}, errors.New("key store unreachable")
}

// Our store failing on the stale-key fallback is marked as ours, but the refusal stays as before.
func TestIssuerVerificationKeysStaleReadFailureKeepsTheRefusal(t *testing.T) {
	t.Parallel()
	assertion := newTestAssertion(t, Type)
	_, store, _, guard, request := newTestValidator(t, assertion.key)
	keys, err := NewIssuerVerificationKeys(&testKeys{key: nil, err: jwks.ErrKeySetUnavailable, calls: 0}, failingCache{})
	require.NoError(t, err)
	validator, err := NewValidator(keys, guard, store)
	require.NoError(t, err)

	_, err = validator.Validate(t.Context(), signTestAssertion(t, assertion.signer, validTestClaims(), validTestExtra()), request)
	require.ErrorIs(t, err, ErrStaleKeysUnreadable)
	require.Equal(t, ReasonKeyUnusable, ReasonOf(err))
	require.False(t, IsOutage(err))
	require.False(t, IsStoreFault(err))
}

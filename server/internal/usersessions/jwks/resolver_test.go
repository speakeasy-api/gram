package jwks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func zeroCacheState() CacheState {
	return CacheState{Document: nil, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}}
}

func TestResolverResolve_FetchesAndParses(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	server.SetETag(`"v1"`)
	resolver := resolverFor(t, server)

	result, err := resolver.Resolve(t.Context(), remoteSourceFor(t, server), zeroCacheState())
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.Len(t, result.KeySet.Keys, 1)
	require.Equal(t, "a", result.KeySet.Keys[0].KeyID)
	require.Equal(t, `"v1"`, result.ETag)
	require.Equal(t, defaultCacheTTL, result.TTL)
	require.NotEmpty(t, result.Document)
	require.Equal(t, 1, server.Fetches())
}

func TestResolverResolve_TTLFromCacheControl(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	server.header.Set("Cache-Control", "max-age=7200")
	resolver := resolverFor(t, server)

	result, err := resolver.Resolve(t.Context(), remoteSourceFor(t, server), zeroCacheState())
	require.NoError(t, err)
	// The httptest server stamps a Date header, so a sliver of apparent age
	// is deducted from the granted two hours.
	require.InDelta(t, (2 * time.Hour).Seconds(), result.TTL.Seconds(), (10 * time.Second).Seconds())
}

func TestResolverResolve_ServesFreshCache(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	resolver := resolverFor(t, server)

	cache := CacheState{
		Document:    keySetJSON(t, testKey(t, "stored")),
		ETag:        `"v1"`,
		ExpiresAt:   time.Now().Add(time.Hour),
		RefreshedAt: time.Now(),
	}
	result, err := resolver.Resolve(t.Context(), remoteSourceFor(t, server), cache)
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeCached, result.Outcome)
	require.Equal(t, "stored", result.KeySet.Keys[0].KeyID)
	require.Zero(t, server.Fetches())
}

func TestResolverResolve_ConditionalNotModified(t *testing.T) {
	t.Parallel()

	stored := keySetJSON(t, testKey(t, "stored"))
	server := newKeySetServer(t, stored)
	server.SetETag(`"v1"`)
	resolver := resolverFor(t, server)

	cache := CacheState{
		Document:    stored,
		ETag:        `"v1"`,
		ExpiresAt:   time.Now().Add(-time.Minute),
		RefreshedAt: time.Now().Add(-time.Hour),
	}
	result, err := resolver.Resolve(t.Context(), remoteSourceFor(t, server), cache)
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeNotModified, result.Outcome)
	require.Equal(t, "stored", result.KeySet.Keys[0].KeyID)
	require.Equal(t, `"v1"`, result.ETag)
	require.Equal(t, defaultCacheTTL, result.TTL)
	require.True(t, bytes.Equal(stored, result.Document))
	require.Equal(t, 1, server.Fetches())
}

func TestResolverResolve_StoredDocumentRescreened(t *testing.T) {
	t.Parallel()

	// A cached document that no longer passes the current screening rules is
	// treated as absent even while its TTL is fresh: the fetch runs and its
	// result is what the caller gets, so a tightened rule takes effect
	// immediately.
	server := newKeySetServer(t, keySetJSON(t, testKey(t, "clean")))
	resolver := resolverFor(t, server)

	cache := CacheState{
		Document:    json.RawMessage(`{"keys":[{"kty":"oct","k":"c2VjcmV0"}]}`),
		ETag:        `"v1"`,
		ExpiresAt:   time.Now().Add(time.Hour),
		RefreshedAt: time.Now(),
	}
	result, err := resolver.Resolve(t.Context(), remoteSourceFor(t, server), cache)
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.Equal(t, "clean", result.KeySet.Keys[0].KeyID)
	require.Equal(t, 1, server.Fetches())
}

func TestResolverResolve_RefusesRedirect(t *testing.T) {
	t.Parallel()

	target := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	redirecting := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL(), http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)

	resolver := newResolver(newFetchClientFrom(redirecting.Client()), testenv.NewMeterProvider(t), testenv.NewLogger(t))
	source, err := NewRemoteSource(redirecting.URL + "/jwks.json")
	require.NoError(t, err)

	_, err = resolver.Resolve(t.Context(), source, zeroCacheState())
	require.ErrorContains(t, err, "status 302")
	require.Zero(t, target.Fetches())
}

func TestResolverResolve_OversizedBodyRejected(t *testing.T) {
	t.Parallel()

	oversized := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(bytes.Repeat([]byte("a"), maxKeySetBytes+1)); err != nil {
			t.Errorf("write oversized response: %v", err)
		}
	}))
	t.Cleanup(oversized.Close)

	resolver := newResolver(newFetchClientFrom(oversized.Client()), testenv.NewMeterProvider(t), testenv.NewLogger(t))
	source, err := NewRemoteSource(oversized.URL + "/jwks.json")
	require.NoError(t, err)

	_, err = resolver.Resolve(t.Context(), source, zeroCacheState())
	require.ErrorIs(t, err, ErrKeySetTooLarge)
}

func TestResolverResolve_Non200IsFetchFailure(t *testing.T) {
	t.Parallel()

	failing := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(failing.Close)

	resolver := newResolver(newFetchClientFrom(failing.Client()), testenv.NewMeterProvider(t), testenv.NewLogger(t))
	source, err := NewRemoteSource(failing.URL + "/jwks.json")
	require.NoError(t, err)

	_, err = resolver.Resolve(t.Context(), source, zeroCacheState())
	require.ErrorContains(t, err, "status 503")
}

func TestResolverResolve_FetchedPrivateMaterialRejected(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, []byte(`{"keys":[{"kty":"EC","crv":"P-256","x":"AA","y":"AA","d":"secret"}]}`))
	resolver := resolverFor(t, server)

	_, err := resolver.Resolve(t.Context(), remoteSourceFor(t, server), zeroCacheState())
	require.ErrorIs(t, err, ErrPrivateKeyMaterial)
}

func TestResolverResolve_InlineSource(t *testing.T) {
	t.Parallel()

	source, err := NewInlineSource(keySetJSON(t, testKey(t, "inline")))
	require.NoError(t, err)
	resolver := newResolver(nil, testenv.NewMeterProvider(t), testenv.NewLogger(t))

	result, err := resolver.Resolve(t.Context(), source, zeroCacheState())
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeInline, result.Outcome)
	require.Equal(t, "inline", result.KeySet.Keys[0].KeyID)
	require.Zero(t, result.TTL)
}

func TestResolverResolve_InlinePrivateMaterialRejected(t *testing.T) {
	t.Parallel()

	source, err := NewInlineSource(json.RawMessage(`{"keys":[{"kty":"oct","k":"c2VjcmV0"}]}`))
	require.NoError(t, err)
	resolver := newResolver(nil, testenv.NewMeterProvider(t), testenv.NewLogger(t))

	_, err = resolver.Resolve(t.Context(), source, zeroCacheState())
	require.ErrorIs(t, err, ErrSymmetricKeyMaterial)
}

func TestResolverResolve_ZeroSourceRejected(t *testing.T) {
	t.Parallel()

	resolver := newResolver(nil, testenv.NewMeterProvider(t), testenv.NewLogger(t))

	_, err := resolver.Resolve(t.Context(), Source{kind: "", inline: nil, uri: "", origin: ""}, zeroCacheState())
	require.ErrorContains(t, err, "zero Source")
}

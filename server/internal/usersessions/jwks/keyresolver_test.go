package jwks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// generousRate is used where rate limiting is not under test.
func generousRate() ratelimit.Rate {
	return ratelimit.PerMinute(1000)
}

func newTestKeyResolver(t *testing.T, server *keySetServer, rate ratelimit.Rate) (*KeyResolver, *MemoryCache) {
	t.Helper()

	cache := NewMemoryCache()
	kr, err := NewKeyResolver(resolverFor(t, server), cache, newTestLimiter(t, rate), testenv.NewLogger(t))
	require.NoError(t, err)
	return kr, cache
}

// primeRotatable stores a fresh-but-not-recently-refreshed entry for source:
// the TTL has not lapsed (so ordinary resolution serves from cache) while the
// refresh cooldown has (so an unknown kid may charge the limiter and consult
// the upstream). This is the steady state a real rotation lands in.
func primeRotatable(t *testing.T, cache *MemoryCache, source Source, document []byte) {
	t.Helper()

	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), CacheState{
		Document:    document,
		ETag:        "",
		ExpiresAt:   time.Now().Add(time.Hour),
		RefreshedAt: time.Now().Add(-time.Hour),
	}))
}

func TestVerificationKey_ColdFetchThenCached(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	kr, _ := newTestKeyResolver(t, server, generousRate())
	source := remoteSourceFor(t, server)

	key, err := kr.VerificationKey(t.Context(), source, "a")
	require.NoError(t, err)
	require.Equal(t, "a", key.KeyID)
	require.Equal(t, 1, server.Fetches())

	key, err = kr.VerificationKey(t.Context(), source, "a")
	require.NoError(t, err)
	require.Equal(t, "a", key.KeyID)
	require.Equal(t, 1, server.Fetches(), "second lookup is served from cache")
}

func TestVerificationKey_UnknownKidAfterFreshFetchIsTerminal(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	kr, _ := newTestKeyResolver(t, server, generousRate())

	_, err := kr.VerificationKey(t.Context(), remoteSourceFor(t, server), "missing")
	require.ErrorIs(t, err, ErrKeyNotFound)
	require.Equal(t, 1, server.Fetches(), "a resolution that just fetched must not fetch again for its own unknown kid")
}

func TestVerificationKey_RotationForcesRefresh(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "rotated")))
	kr, cache := newTestKeyResolver(t, server, generousRate())
	source := remoteSourceFor(t, server)
	primeRotatable(t, cache, source, keySetJSON(t, testKey(t, "old")))

	key, err := kr.VerificationKey(t.Context(), source, "rotated")
	require.NoError(t, err)
	require.Equal(t, "rotated", key.KeyID)
	require.Equal(t, 1, server.Fetches(), "the fresh TTL is bypassed for exactly one forced refresh")
}

func TestVerificationKey_NegativeCacheAfterRefresh(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	kr, _ := newTestKeyResolver(t, server, generousRate())
	source := remoteSourceFor(t, server)

	// The cold fetch consults the upstream and stamps RefreshedAt.
	_, err := kr.VerificationKey(t.Context(), source, "a")
	require.NoError(t, err)

	// Probes with fabricated kids inside the cooldown answer from the
	// just-confirmed set: no fetch, no limiter spend.
	for _, kid := range []string{"probe-1", "probe-2", "probe-3"} {
		_, err = kr.VerificationKey(t.Context(), source, kid)
		require.ErrorIs(t, err, ErrKeyNotFound)
	}
	require.Equal(t, 1, server.Fetches())
}

func TestVerificationKey_RefreshRateLimited(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "b")))
	kr, cache := newTestKeyResolver(t, server, ratelimit.PerMinute(1))
	source := remoteSourceFor(t, server)
	primeRotatable(t, cache, source, keySetJSON(t, testKey(t, "a")))

	// The single budgeted refresh serves a real rotation.
	key, err := kr.VerificationKey(t.Context(), source, "b")
	require.NoError(t, err)
	require.Equal(t, "b", key.KeyID)

	// Age the entry past the cooldown again; the bucket is now empty, so the
	// next unknown kid is refused without a fetch.
	server.SetBody(keySetJSON(t, testKey(t, "c")))
	primeRotatable(t, cache, source, keySetJSON(t, testKey(t, "b")))

	_, err = kr.VerificationKey(t.Context(), source, "c")
	require.ErrorIs(t, err, ErrRefreshRateLimited)
	require.Equal(t, 1, server.Fetches())
}

func TestVerificationKey_ConcurrentRotationCoalesces(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "rotated")))
	// Burst 1: coalescing is what keeps a burst of rotated assertions from
	// needing more than a single token.
	kr, cache := newTestKeyResolver(t, server, ratelimit.PerMinute(1))
	source := remoteSourceFor(t, server)
	primeRotatable(t, cache, source, keySetJSON(t, testKey(t, "old")))

	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range errs {
		wg.Go(func() {
			key, err := kr.VerificationKey(t.Context(), source, "rotated")
			if err == nil && key.KeyID != "rotated" {
				err = errors.New("wrong key selected")
			}
			errs[i] = err
		})
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "caller %d", i)
	}
	require.Equal(t, 1, server.Fetches(), "one forced refresh serves every concurrent caller")
}

func TestVerificationKey_InlineUnknownKidTerminal(t *testing.T) {
	t.Parallel()

	source, err := NewInlineSource(keySetJSON(t, testKey(t, "a")))
	require.NoError(t, err)
	kr, err := NewKeyResolver(
		newResolver(nil, testenv.NewMeterProvider(t), testenv.NewLogger(t)),
		NewMemoryCache(),
		newTestLimiter(t, generousRate()),
		testenv.NewLogger(t),
	)
	require.NoError(t, err)

	key, err := kr.VerificationKey(t.Context(), source, "a")
	require.NoError(t, err)
	require.Equal(t, "a", key.KeyID)

	_, err = kr.VerificationKey(t.Context(), source, "missing")
	require.ErrorIs(t, err, ErrKeyNotFound)
}

// failingCache errors on Get or Put to exercise the fail-closed and
// log-only paths.
type failingCache struct {
	getErr error
	putErr error
	inner  Cache
}

var _ Cache = (*failingCache)(nil)

func (c *failingCache) Get(ctx context.Context, key string) (CacheState, error) {
	if c.getErr != nil {
		return CacheState{Document: nil, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}}, c.getErr
	}
	state, err := c.inner.Get(ctx, key)
	if err != nil {
		return state, fmt.Errorf("inner cache get: %w", err)
	}
	return state, nil
}

func (c *failingCache) Put(ctx context.Context, key string, state CacheState) error {
	if c.putErr != nil {
		return c.putErr
	}
	if err := c.inner.Put(ctx, key, state); err != nil {
		return fmt.Errorf("inner cache put: %w", err)
	}
	return nil
}

func TestVerificationKey_CacheReadErrorFailsClosed(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	kr, err := NewKeyResolver(
		resolverFor(t, server),
		&failingCache{getErr: errors.New("store down"), putErr: nil, inner: nil},
		newTestLimiter(t, generousRate()),
		testenv.NewLogger(t),
	)
	require.NoError(t, err)

	_, err = kr.VerificationKey(t.Context(), remoteSourceFor(t, server), "a")
	require.ErrorContains(t, err, "read key set cache")
	require.Zero(t, server.Fetches(), "a broken store must not degrade into an upstream fetch")
}

func TestVerificationKey_CacheWriteFailureDoesNotFailResolution(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	kr, err := NewKeyResolver(
		resolverFor(t, server),
		&failingCache{getErr: nil, putErr: errors.New("store down"), inner: NewMemoryCache()},
		newTestLimiter(t, generousRate()),
		testenv.NewLogger(t),
	)
	require.NoError(t, err)

	key, err := kr.VerificationKey(t.Context(), remoteSourceFor(t, server), "a")
	require.NoError(t, err)
	require.Equal(t, "a", key.KeyID)
}

func TestVerificationKey_LimiterErrorFailsClosed(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "b")))
	// An invalid rate makes Allow return an error on every call, standing in
	// for a limiter store outage.
	broken := ratelimit.New(nil, t.Name(), ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0})
	cache := NewMemoryCache()
	kr, err := NewKeyResolver(resolverFor(t, server), cache, broken, testenv.NewLogger(t))
	require.NoError(t, err)
	source := remoteSourceFor(t, server)
	primeRotatable(t, cache, source, keySetJSON(t, testKey(t, "a")))

	_, err = kr.VerificationKey(t.Context(), source, "b")
	require.ErrorContains(t, err, "rate limiter")
	require.Zero(t, server.Fetches(), "a limiter outage fails closed, never open")
}

func TestNewKeyResolver_RequiresDependencies(t *testing.T) {
	t.Parallel()

	resolver := newResolver(nil, testenv.NewMeterProvider(t), testenv.NewLogger(t))
	limiter := newTestLimiter(t, generousRate())
	logger := testenv.NewLogger(t)

	_, err := NewKeyResolver(nil, NewMemoryCache(), limiter, logger)
	require.Error(t, err)
	_, err = NewKeyResolver(resolver, nil, limiter, logger)
	require.Error(t, err)
	_, err = NewKeyResolver(resolver, NewMemoryCache(), nil, logger)
	require.Error(t, err)
	_, err = NewKeyResolver(resolver, NewMemoryCache(), limiter, nil)
	require.Error(t, err)
}

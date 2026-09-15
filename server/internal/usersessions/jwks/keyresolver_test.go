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
	kr, err := NewKeyResolver(resolverFor(t, server), cache, newTestLimiter(t, rate), nil, testenv.NewLogger(t))
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

// The scope budget bounds fetches across every source a scope names. Two
// never-seen sources in one scope with a budget of one: the first cold fetch
// spends it and the second is refused before any request leaves. A different
// scope naming the same second source is unaffected, and the source's cached
// entry is then shared back with the first scope, since the cache is keyed
// by URL and the scope plays no part in it.
func TestVerificationKey_FetchScopeBudget(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	cache := NewMemoryCache()
	kr, err := NewKeyResolver(
		resolverFor(t, server),
		cache,
		newTestLimiter(t, generousRate()),
		newTestLimiter(t, ratelimit.PerMinute(1)),
		testenv.NewLogger(t),
	)
	require.NoError(t, err)

	// Two sources on one host (the host serves the same set at any path),
	// so both are reachable and only the budget can refuse the second.
	sourceA := remoteSourceFor(t, server).WithFetchScope("tenant-1")
	sourceB, err := NewRemoteSource(server.server.URL + "/second.json")
	require.NoError(t, err)

	key, err := kr.VerificationKey(t.Context(), sourceA, "a")
	require.NoError(t, err)
	require.Equal(t, "a", key.KeyID)
	require.Equal(t, 1, server.Fetches())

	_, err = kr.VerificationKey(t.Context(), sourceB.WithFetchScope("tenant-1"), "a")
	require.ErrorIs(t, err, ErrFetchRateLimited)
	require.Equal(t, 1, server.Fetches(), "a scope out of budget must not reach the upstream")

	// A different scope has its own budget, so the same source resolves.
	_, err = kr.VerificationKey(t.Context(), sourceB.WithFetchScope("tenant-2"), "a")
	require.NoError(t, err)
	require.Equal(t, 2, server.Fetches())

	// The entry that fetch stored is keyed by URL, so the first scope now
	// reads it warm: no fetch, no charge, and no budget to be refused by.
	_, err = kr.VerificationKey(t.Context(), sourceB.WithFetchScope("tenant-1"), "a")
	require.NoError(t, err)
	for range 3 {
		_, err = kr.VerificationKey(t.Context(), sourceA, "a")
		require.NoError(t, err)
	}
	require.Equal(t, 2, server.Fetches())
}

// A stored document that no longer passes screening is refetched, and that
// refetch is charged to the scope like any other upstream request: the charge
// follows the request, not the cache condition that caused it.
func TestVerificationKey_FetchScopeBudgetCoversRescreenFailure(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	cache := NewMemoryCache()
	kr, err := NewKeyResolver(
		resolverFor(t, server),
		cache,
		newTestLimiter(t, generousRate()),
		newTestLimiter(t, ratelimit.PerMinute(1)),
		testenv.NewLogger(t),
	)
	require.NoError(t, err)

	// Spend the scope's budget on an unrelated cold fetch.
	other, err := NewRemoteSource(server.server.URL + "/other.json")
	require.NoError(t, err)
	_, err = kr.VerificationKey(t.Context(), other.WithFetchScope("tenant-3"), "a")
	require.NoError(t, err)
	require.Equal(t, 1, server.Fetches())

	// A fresh-looking entry whose document is unusable: the resolver must
	// consult the upstream, and the scope has nothing left to pay for it.
	source := remoteSourceFor(t, server).WithFetchScope("tenant-3")
	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), CacheState{
		Document:    []byte(`{"keys":`),
		ETag:        "",
		ExpiresAt:   time.Now().Add(time.Hour),
		RefreshedAt: time.Now(),
	}))
	_, err = kr.VerificationKey(t.Context(), source, "a")
	require.ErrorIs(t, err, ErrFetchRateLimited)
	require.Equal(t, 1, server.Fetches(), "an uncharged refetch would be a budget bypass")
}

// A forced refresh the scope's budget refuses never reached the origin, so
// it must not stamp the refresh cooldown: the source's stored state stays
// exactly as it was, and a legitimate refresh a moment later (once the scope
// has budget again) is not suppressed for the whole window.
func TestVerificationKey_ScopeDenialDoesNotStampCooldown(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a"), testKey(t, "b")))
	cache := NewMemoryCache()
	kr, err := NewKeyResolver(
		resolverFor(t, server),
		cache,
		newTestLimiter(t, generousRate()),
		newTestLimiter(t, ratelimit.PerMinute(1)),
		testenv.NewLogger(t),
	)
	require.NoError(t, err)

	// Spend the scope's budget on an unrelated cold fetch.
	other, err := NewRemoteSource(server.server.URL + "/other.json")
	require.NoError(t, err)
	_, err = kr.VerificationKey(t.Context(), other.WithFetchScope("tenant-6"), "a")
	require.NoError(t, err)

	source := remoteSourceFor(t, server).WithFetchScope("tenant-6")
	primeRotatable(t, cache, source, keySetJSON(t, testKey(t, "a")))
	before, err := cache.Get(t.Context(), source.CacheKey())
	require.NoError(t, err)

	_, err = kr.VerificationKey(t.Context(), source, "b")
	require.ErrorIs(t, err, ErrFetchRateLimited)

	after, err := cache.Get(t.Context(), source.CacheKey())
	require.NoError(t, err)
	require.Equal(t, before.RefreshedAt, after.RefreshedAt, "a denial at admission is not a consult and must not enter the cooldown")
	require.Equal(t, before.Document, after.Document)
}

// A forced refresh the source's own budget refuses costs the scope nothing:
// unknown-kid probes against an exhausted source must not drain the budget
// that source's neighbours share. Proven by spending the source's budget,
// probing it again, and then showing the scope still has the token a
// neighbouring cold fetch needs.
func TestVerificationKey_RefusedRefreshDoesNotChargeScope(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a"), testKey(t, "b")))
	cache := NewMemoryCache()
	kr, err := NewKeyResolver(
		resolverFor(t, server),
		cache,
		newTestLimiter(t, ratelimit.PerMinute(1)),
		newTestLimiter(t, ratelimit.PerMinute(2)),
		testenv.NewLogger(t),
	)
	require.NoError(t, err)

	// One forced refresh spends the source's whole budget and one of the
	// scope's two tokens.
	probed := remoteSourceFor(t, server).WithFetchScope("tenant-4")
	primeRotatable(t, cache, probed, keySetJSON(t, testKey(t, "a")))
	_, err = kr.VerificationKey(t.Context(), probed, "b")
	require.NoError(t, err)
	require.Equal(t, 1, server.Fetches())

	// Age it past the cooldown and probe an unknown kid again: the source's
	// budget refuses before any request is issued.
	primeRotatable(t, cache, probed, keySetJSON(t, testKey(t, "a")))
	_, err = kr.VerificationKey(t.Context(), probed, "c")
	require.ErrorIs(t, err, ErrRefreshRateLimited)
	require.Equal(t, 1, server.Fetches())

	// The scope's second token must still be there for a neighbour's cold
	// fetch. Had the refused refresh charged the scope, this would be
	// ErrFetchRateLimited.
	neighbour, err := NewRemoteSource(server.server.URL + "/neighbour.json")
	require.NoError(t, err)
	_, err = kr.VerificationKey(t.Context(), neighbour.WithFetchScope("tenant-4"), "a")
	require.NoError(t, err, "a refused refresh must not have spent the scope's remaining token")
	require.Equal(t, 2, server.Fetches())
}

// The scope budget is charged on the forced unknown-kid refresh as well as
// on cold fetches, and before the per-source refresh budget, so a scope that
// has spent its budget on one source cannot force a refresh of another.
func TestVerificationKey_FetchScopeBudgetCoversRefresh(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a"), testKey(t, "b")))
	cache := NewMemoryCache()
	kr, err := NewKeyResolver(
		resolverFor(t, server),
		cache,
		newTestLimiter(t, generousRate()),
		newTestLimiter(t, ratelimit.PerMinute(1)),
		testenv.NewLogger(t),
	)
	require.NoError(t, err)

	// One cold fetch spends the scope's whole budget.
	cold := remoteSourceFor(t, server).WithFetchScope("tenant-2")
	_, err = kr.VerificationKey(t.Context(), cold, "a")
	require.NoError(t, err)
	require.Equal(t, 1, server.Fetches())

	// A second source on the same host, primed so only a forced refresh
	// could learn its new kid, is refused before the upstream is asked.
	rotating, err := NewRemoteSource(server.server.URL + "/rotating.json")
	require.NoError(t, err)
	rotating = rotating.WithFetchScope("tenant-2")
	primeRotatable(t, cache, rotating, keySetJSON(t, testKey(t, "a")))

	_, err = kr.VerificationKey(t.Context(), rotating, "b")
	require.ErrorIs(t, err, ErrFetchRateLimited)
	require.Equal(t, 1, server.Fetches(), "the refused refresh must not reach the upstream")
}

func TestVerificationKey_FailedConsultEntersCooldown(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	kr, cache := newTestKeyResolver(t, server, ratelimit.PerMinute(1))
	source := remoteSourceFor(t, server)
	primeRotatable(t, cache, source, keySetJSON(t, testKey(t, "a")))
	server.SetStatus(500)

	// The first probe charges the limiter and consults the failing origin.
	_, err := kr.VerificationKey(t.Context(), source, "unknown")
	require.ErrorContains(t, err, "status 500")
	require.Equal(t, 1, server.Fetches())

	// Follow-up probes inside the cooldown answer from the stored set with
	// no fetch and no limiter spend: with a burst-1 bucket already empty,
	// reaching the limiter would surface ErrRefreshRateLimited instead.
	_, err = kr.VerificationKey(t.Context(), source, "unknown")
	require.ErrorIs(t, err, ErrKeyNotFound)
	require.Equal(t, 1, server.Fetches(), "a failed consult negative-caches like a successful one")
}

func TestVerificationKey_UnusableStoredValidatorIsDropped(t *testing.T) {
	t.Parallel()

	server := newKeySetServer(t, keySetJSON(t, testKey(t, "a")))
	kr, cache := newTestKeyResolver(t, server, generousRate())
	source := remoteSourceFor(t, server)

	// A stored validator today's screening rules reject: unquoted, so it is
	// not a well-formed entity-tag. Storage is shared across replicas and
	// outlives any one binary, so a value written under laxer rules is the
	// case this covers.
	require.NoError(t, cache.Put(t.Context(), source.CacheKey(), CacheState{
		Document:    keySetJSON(t, testKey(t, "a")),
		ETag:        "unquoted-validator",
		ExpiresAt:   time.Now().Add(time.Hour),
		RefreshedAt: time.Now().Add(-time.Hour),
	}))

	// An unknown kid forces a consult; failing it exercises the cooldown
	// marker, the one write path that carries the stored validator forward.
	server.SetStatus(500)
	_, err := kr.VerificationKey(t.Context(), source, "unknown")
	require.ErrorContains(t, err, "status 500")

	state, err := cache.Get(t.Context(), source.CacheKey())
	require.NoError(t, err)
	require.Empty(t, state.ETag, "an unusable stored validator is dropped, not replayed and re-persisted")
	require.NotEmpty(t, state.Document, "dropping the validator must not discard the document it identified")
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
		nil,
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
		nil,
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
		nil,
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
	kr, err := NewKeyResolver(resolverFor(t, server), cache, broken, nil, testenv.NewLogger(t))
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

	_, err := NewKeyResolver(nil, NewMemoryCache(), limiter, nil, logger)
	require.Error(t, err)
	_, err = NewKeyResolver(resolver, nil, limiter, nil, logger)
	require.Error(t, err)
	_, err = NewKeyResolver(resolver, NewMemoryCache(), nil, nil, logger)
	require.Error(t, err)
	_, err = NewKeyResolver(resolver, NewMemoryCache(), limiter, nil, nil)
	require.Error(t, err)
}

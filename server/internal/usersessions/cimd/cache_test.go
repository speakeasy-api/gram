// Tests for the document cache layered over the fetch lifecycle: the
// short-circuit on a fresh cache, the conditional refresh when it has
// lapsed, and the outcomes each produces for the caller to persist.

package cimd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// conditionalDocServer hosts a document that carries an ETag and answers a
// matching If-None-Match with 304, so a test can drive a full revalidation
// round trip. Its counters and mutable response settings are guarded because
// the handler runs on the server's own goroutine.
type conditionalDocServer struct {
	srv      *httptest.Server
	resolver *Resolver
	clientID string

	mu sync.Mutex
	// etag is served on 200 responses and matched against If-None-Match.
	// Empty means the host offers no validator at all.
	etag string
	// notModifiedETag is served on 304 responses when non-empty, standing
	// in for a host that supersedes its validator on revalidation.
	notModifiedETag string
	// cacheControl is sent verbatim as the Cache-Control header when
	// non-empty.
	cacheControl string
	// requests counts every request the host received, and
	// conditionalRequests only those carrying If-None-Match.
	requests            int
	conditionalRequests int
}

func newConditionalDocServer(t *testing.T, etag string) *conditionalDocServer {
	t.Helper()

	ds := &conditionalDocServer{
		srv:                 nil,
		resolver:            nil,
		clientID:            "",
		mu:                  sync.Mutex{},
		etag:                etag,
		notModifiedETag:     "",
		cacheControl:        "",
		requests:            0,
		conditionalRequests: 0,
	}

	ds.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ds.mu.Lock()
		ds.requests++
		conditional := r.Header.Get("If-None-Match")
		if conditional != "" {
			ds.conditionalRequests++
		}
		etag, notModifiedETag, cacheControl := ds.etag, ds.notModifiedETag, ds.cacheControl
		clientID := ds.clientID
		ds.mu.Unlock()

		if cacheControl != "" {
			w.Header().Set("Cache-Control", cacheControl)
		}
		if conditional != "" && conditional == etag {
			if notModifiedETag != "" {
				w.Header().Set("ETag", notModifiedETag)
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(validDocumentJSON(clientID)); err != nil {
			t.Errorf("encode document: %v", err)
		}
	}))
	t.Cleanup(ds.srv.Close)

	ds.resolver = newResolver(newFetchClientFrom(ds.srv.Client()), testenv.NewMeterProvider(t), testenv.NewLogger(t))
	ds.clientID = ds.srv.URL + "/client.json"
	return ds
}

func (ds *conditionalDocServer) counts(t *testing.T) (total int, conditional int) {
	t.Helper()

	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.requests, ds.conditionalRequests
}

func (ds *conditionalDocServer) set(t *testing.T, mutate func(ds *conditionalDocServer)) {
	t.Helper()

	ds.mu.Lock()
	defer ds.mu.Unlock()
	mutate(ds)
}

func TestResolve_FreshCacheSkipsFetch(t *testing.T) {
	t.Parallel()

	ds := newConditionalDocServer(t, `"v1"`)

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, CacheState{
		ExpiresAt: time.Now().Add(time.Hour),
		ETag:      `"v1"`,
	})
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeCached, result.Outcome)
	require.Nil(t, result.Document, "a cache hit parses nothing; the caller's row is the document")
	require.Equal(t, `"v1"`, result.ETag, "the stored validator survives a cache hit")
	require.Zero(t, result.TTL, "a cache hit leaves the stored expiry alone")

	total, _ := ds.counts(t)
	require.Zero(t, total, "a fresh cache must not reach the document host at all")
}

func TestResolve_ExpiredCacheRefetches(t *testing.T) {
	t.Parallel()

	ds := newConditionalDocServer(t, "")

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, CacheState{
		ExpiresAt: time.Now().Add(-time.Minute),
		ETag:      "",
	})
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.NotNil(t, result.Document)

	total, conditional := ds.counts(t)
	require.Equal(t, 1, total)
	require.Zero(t, conditional, "no stored validator means an unconditional refresh")
}

func TestResolve_NotModifiedKeepsCachedDocument(t *testing.T) {
	t.Parallel()

	ds := newConditionalDocServer(t, `"v1"`)
	ds.set(t, func(ds *conditionalDocServer) { ds.cacheControl = "max-age=7200" })

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, CacheState{
		ExpiresAt: time.Now().Add(-time.Minute),
		ETag:      `"v1"`,
	})
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeNotModified, result.Outcome)
	require.Nil(t, result.Document, "a 304 has no body to parse; the caller's row stands")
	require.Equal(t, `"v1"`, result.ETag)
	// Not exactly two hours: the server stamps a Date, HTTP-date has
	// one-second granularity, and the apparent age that implies is deducted.
	require.InDelta(t, (2 * time.Hour).Seconds(), result.TTL.Seconds(), 2, "the 304's own freshness headers set the new expiry")

	total, conditional := ds.counts(t)
	require.Equal(t, 1, total)
	require.Equal(t, 1, conditional, "a stored validator must be replayed in If-None-Match")
}

func TestResolve_NotModifiedAdoptsSupersedingETag(t *testing.T) {
	t.Parallel()

	// RFC 9110 §15.4.5 lets a 304 carry a new validator. Ignoring it would
	// revalidate against the superseded one forever.
	ds := newConditionalDocServer(t, `"v1"`)
	ds.set(t, func(ds *conditionalDocServer) { ds.notModifiedETag = `"v2"` })

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, CacheState{
		ExpiresAt: time.Now().Add(-time.Minute),
		ETag:      `"v1"`,
	})
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeNotModified, result.Outcome)
	require.Equal(t, `"v2"`, result.ETag)
}

func TestResolve_NotModifiedKeepsStoredETagWhenUnusable(t *testing.T) {
	t.Parallel()

	ds := newConditionalDocServer(t, `"v1"`)
	ds.set(t, func(ds *conditionalDocServer) { ds.notModifiedETag = `"` + strings.Repeat("a", maxETagLength) + `"` })

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, CacheState{
		ExpiresAt: time.Now().Add(-time.Minute),
		ETag:      `"v1"`,
	})
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeNotModified, result.Outcome)
	require.Equal(t, `"v1"`, result.ETag, "an unusable replacement leaves the working validator in place")
}

func TestResolve_ConditionalRequestAnsweredWith200Replaces(t *testing.T) {
	t.Parallel()

	// The rotation path: the stored validator no longer matches, so the host
	// sends a body instead of a 304 and the new validator replaces the old.
	ds := newConditionalDocServer(t, `"v2"`)

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, CacheState{
		ExpiresAt: time.Now().Add(-time.Minute),
		ETag:      `"v1"`,
	})
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.NotNil(t, result.Document)
	require.Equal(t, `"v2"`, result.ETag)

	_, conditional := ds.counts(t)
	require.Equal(t, 1, conditional, "the stale validator is still offered; the host decides")
}

func TestResolve_RefreshClearsStoredETagWhenHostStopsSendingOne(t *testing.T) {
	t.Parallel()

	// A host that drops its ETag must not leave the old one stored: the next
	// refresh would revalidate against a validator the host no longer knows,
	// and a buggy host answering 304 to it would pin the old document.
	ds := newConditionalDocServer(t, "")

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, CacheState{
		ExpiresAt: time.Now().Add(-time.Minute),
		ETag:      `"v1"`,
	})
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.Empty(t, result.ETag, "the refreshed document carries no validator, so none is kept")
}

func TestResolve_UnconditionalRequestRejectsNotModified(t *testing.T) {
	t.Parallel()

	// A host that answers 304 to a request carrying no If-None-Match is
	// broken or intermediary-fronted. Treating it as a cache confirmation
	// would confirm a document this resolver has never seen.
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})

	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)
	require.ErrorContains(t, err, "status 304")
}

func TestResolve_RefreshCarriesETagAndTTL(t *testing.T) {
	t.Parallel()

	ds := newConditionalDocServer(t, `"v1"`)
	ds.set(t, func(ds *conditionalDocServer) { ds.cacheControl = "public, max-age=7200" })

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, noCache)
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.Equal(t, `"v1"`, result.ETag)
	require.InDelta(t, (2 * time.Hour).Seconds(), result.TTL.Seconds(), 2)
}

func TestResolve_RefreshDropsOversizedETag(t *testing.T) {
	t.Parallel()

	// The validator is chosen by the document host and lands in a TEXT
	// column, so an oversized one is dropped and the next refresh is
	// unconditional rather than replaying megabytes.
	ds := newConditionalDocServer(t, `"`+strings.Repeat("a", maxETagLength)+`"`)

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, noCache)
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.Empty(t, result.ETag)
}

func TestResolve_RefreshWithoutETagReportsNoValidator(t *testing.T) {
	t.Parallel()

	ds := newConditionalDocServer(t, "")

	result, err := ds.resolver.Resolve(t.Context(), ds.clientID, noCache)
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.Empty(t, result.ETag)
	require.Equal(t, defaultCacheTTL, result.TTL)
}

func TestResolve_RefreshFailureReturnsNoResult(t *testing.T) {
	t.Parallel()

	// Fail-closed: a lapsed cache whose refresh fails yields an error and
	// nothing to persist, so the caller's stored row is left exactly as it
	// was rather than being extended or served stale (-02 §5.1).
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream is down", http.StatusInternalServerError)
	})

	result, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", CacheState{
		ExpiresAt: time.Now().Add(-time.Minute),
		ETag:      `"v1"`,
	})
	require.ErrorContains(t, err, "status 500")
	require.Nil(t, result)
}

func TestResolve_FreshCacheStillRejectsInvalidClientIDURL(t *testing.T) {
	t.Parallel()

	// Freshness is consulted only after §3 syntax has passed, so a stored
	// row can never keep a client_id alive that current rules reject.
	requests := 0
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
	})

	_, err := resolver.Resolve(t.Context(), srv.URL, CacheState{
		ExpiresAt: time.Now().Add(time.Hour),
		ETag:      `"v1"`,
	})
	requireOAuthError(t, err, "invalid_request")
	require.Zero(t, requests)
}

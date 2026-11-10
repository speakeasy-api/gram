package internalsessionissuers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDiscoveryRefusesMissesWhenEveryFillSlotIsTaken(t *testing.T) {
	t.Parallel()
	h := saturatedHandler(t)
	response := httptest.NewRecorder()

	err := h.serve(response, discoveryRequest("org_busy"), false)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code, "a saturated process refuses instead of queueing on the pool")
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	_, cached := h.cache.Get(documentKey{organizationID: "org_busy", keys: false})
	require.False(t, cached, "a refusal is never cached")
}

// saturatedHandler has no database and no free fill slot, so any request that
// gets past validation and the caches is refused.
func saturatedHandler(t *testing.T) *handler {
	t.Helper()
	h := &handler{
		db:      nil,
		logger:  testenv.NewLogger(t),
		cache:   expirable.NewLRU[documentKey, cachedDocument](documentCacheSize, nil, documentCacheTTL),
		missing: expirable.NewLRU[documentKey, struct{}](missingCacheSize, nil, documentCacheTTL),
		flights: singleflight.Group{},
		fills:   make(chan struct{}, 1),
		now:     time.Now,
	}
	h.fills <- struct{}{}
	return h
}

func discoveryRequest(organizationID string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("organizationID", organizationID)
	request := httptest.NewRequest(http.MethodGet, "/oauth/idp/org/x", nil)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeCtx))
}

func TestDiscoveryRejectsMalformedIDsBeforeCacheOrDatabase(t *testing.T) {
	t.Parallel()
	h := saturatedHandler(t)
	for _, id := range []string{"org.with.dots", "org%2Fslash", strings.Repeat("a", 129), "org id"} {
		err := h.serve(httptest.NewRecorder(), discoveryRequest(id), false)
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeNotFound, shareable.Code, "a malformed id never waits for a fill slot")
	}
	require.Zero(t, h.cache.Len())
	require.Zero(t, h.missing.Len())
}

func TestDiscoveryServesMissesFromTheirOwnCache(t *testing.T) {
	t.Parallel()
	h := saturatedHandler(t)
	h.missing.Add(documentKey{organizationID: "org_gone", keys: false}, struct{}{})

	response := httptest.NewRecorder()
	err := h.serve(response, discoveryRequest("org_gone"), false)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeNotFound, shareable.Code, "a cached miss needs no fill slot")
	require.Zero(t, h.cache.Len(), "misses never occupy the document cache")
}

func TestDiscoveryTreatsLostDatabaseConnectionsAsRetryable(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"57P01", "57P03", "08006", "53300"} {
		require.True(t, transientDatabaseError(fmt.Errorf("load issuer: %w", &pgconn.PgError{Code: code})), code)
	}
	require.False(t, transientDatabaseError(&pgconn.PgError{Code: "23505"}))
	require.False(t, transientDatabaseError(errors.New("decode issuer keys")))
}

func TestDiscoveryKeepsServingACachedKeySetWhileTheCheckIsBusy(t *testing.T) {
	t.Parallel()
	h := saturatedHandler(t)
	now := time.Now()
	h.now = func() time.Time { return now }
	checkedAt := new(atomic.Int64)
	checkedAt.Store(now.Add(-keysRevalidateAfter).UnixNano())
	key := documentKey{organizationID: "org_keys", keys: true}
	h.cache.Add(key, cachedDocument{body: []byte(`{"keys":[]}`), renderedAt: now.Add(-keysRevalidateAfter), version: keysVersion{}, checkedAt: checkedAt})

	response := httptest.NewRecorder()
	require.NoError(t, h.serve(response, discoveryRequest("org_keys"), true))
	require.Equal(t, http.StatusOK, response.Code, "a check that can't get a fill slot serves the cached key set until its TTL")
	require.JSONEq(t, `{"keys":[]}`, response.Body.String())
	require.Equal(t, now.Add(-keysRevalidateAfter).UnixNano(), checkedAt.Load(), "a skipped check is retried on the next request")
}

func TestDiscoveryServesARecentlyCheckedKeySetWithoutTheDatabase(t *testing.T) {
	t.Parallel()
	h := saturatedHandler(t)
	now := time.Now()
	h.now = func() time.Time { return now }
	checkedAt := new(atomic.Int64)
	checkedAt.Store(now.UnixNano())
	h.cache.Add(documentKey{organizationID: "org_keys", keys: true}, cachedDocument{body: []byte(`{"keys":[]}`), renderedAt: now, version: keysVersion{}, checkedAt: checkedAt})

	response := httptest.NewRecorder()
	require.NoError(t, h.serve(response, discoveryRequest("org_keys"), true))
	require.Equal(t, http.StatusOK, response.Code)
}

package internalsessionissuers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDiscoveryRefusesMissesWhenEveryFillSlotIsTaken(t *testing.T) {
	t.Parallel()
	h := &handler{db: nil, logger: testenv.NewLogger(t), cache: expirable.NewLRU[documentKey, cachedDocument](documentCacheSize, nil, documentCacheTTL), flights: singleflight.Group{}, fills: make(chan struct{}, 1)}
	h.fills <- struct{}{}

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("organizationID", "org_busy")
	request := httptest.NewRequest(http.MethodGet, "/oauth/idp/org/org_busy", nil)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeCtx))
	response := httptest.NewRecorder()

	err := h.serve(response, request, false)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code, "a saturated process refuses instead of queueing on the pool")
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	_, cached := h.cache.Get(documentKey{organizationID: "org_busy", keys: false})
	require.False(t, cached, "a refusal is never cached")
}

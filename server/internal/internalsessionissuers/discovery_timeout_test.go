package internalsessionissuers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestDiscoveryPoolTimeoutIsRetryableAndNotCached(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t, true)
	cfg := ti.conn.Config().Copy()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	held, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer held.Release()
	mux := goahttp.NewMuxer()
	internalsessionissuers.AttachDiscovery(mux, pool, testenv.NewLogger(t))
	path := "/oauth/idp/org/" + ti.orgID
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	held.Release()
	provisionIssuer(t, ctx, ti)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, response.Code, "retry serves the newly provisioned issuer rather than caching the timeout")
}

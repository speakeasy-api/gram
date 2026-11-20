package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAttachedRoutesReadOneAICatalogSnapshot(t *testing.T) {
	t.Parallel()

	db, err := TestInfra.CloneTestDatabase(t, "mcp_ai_catalog_pin")
	require.NoError(t, err)
	require.NoError(t, catalogseed.EnsureCatalog(t.Context(), db))
	store := aivendorstest.NewStore()
	logger := testenv.NewLogger(t)
	mux := goahttp.NewMuxer()
	var first, refreshed, second *aivendors.Snapshot
	var refreshErr error
	aiCatalogMux{Muxer: mux, store: store}.Handle(http.MethodGet, "/pinned", func(w http.ResponseWriter, r *http.Request) {
		first = store.SnapshotFor(r.Context())
		refreshErr = store.Refresh(r.Context(), db, logger, aitargets.ValidateCatalog)
		refreshed = store.Snapshot()
		second = store.SnapshotFor(r.Context())
	})
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/pinned", nil))
	require.NoError(t, refreshErr)
	require.NotSame(t, first, refreshed, "the refresh must publish a new snapshot")
	require.Same(t, first, second)
}

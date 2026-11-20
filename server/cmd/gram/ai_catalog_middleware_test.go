package gram

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMCPMuxPinsOneAICatalogSnapshotPerRequest(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("mcp-ai-catalog", flag.ContinueOnError)
	flags.String("server-url", "https://app.example.com", "")
	flags.String("site-url", "https://app.example.com", "")
	flags.String("environment", "prod", "")
	origin, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	host, err := mcp.NewAuthenticationHost("https://auth.example.com", origin, "prod")
	require.NoError(t, err)
	store := aivendorstest.NewStore()
	mux, err := newMCPServerMux(cli.NewContext(nil, flags, nil), testenv.NewLogger(t), nil, origin, store, host, nil, nil, &background.Publishers{})
	require.NoError(t, err)

	snapshotNamed := func(name string) *aivendors.Snapshot {
		products := aivendors.Products()
		products[0].DisplayName = name
		return aivendors.NewSnapshot(products)
	}
	var next *aivendors.Snapshot
	var first, second *aivendors.Snapshot
	swapMidRequest := func(w http.ResponseWriter, r *http.Request) {
		first = store.SnapshotFor(r.Context())
		store.Publish(next)
		second = store.SnapshotFor(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}
	mux.Handle(http.MethodGet, "/test-ai-catalog", swapMidRequest)
	host.Handle(http.MethodGet, "/test-ai-catalog", swapMidRequest)

	for i, target := range []string{origin.String(), "https://auth.example.com"} {
		before := store.Snapshot()
		next = snapshotNamed(fmt.Sprintf("Swapped %d", i))
		first, second = nil, nil
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target+"/test-ai-catalog", nil))
		require.Equal(t, http.StatusNoContent, response.Code, target)
		require.Same(t, before, first, "a request starts on the snapshot published before it: %s", target)
		require.Same(t, first, second, target)
		require.Same(t, next, store.Snapshot(), target)
	}
}

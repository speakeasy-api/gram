package localfixture

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRegistryHTTPServesReviewedCatalogContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	origin, err := url.Parse(server.URL)
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	server.Config.Handler = NewRegistryHTTP(config).Handler()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	registryPolicy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{},
		guardian.WithTLSRootCAs(roots),
	)
	require.NoError(t, err)
	registryClient := externalmcp.NewRegistryClient(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		registryPolicy,
		registryBackend{},
		cache.NoopCache,
	)

	catalog := platformmcp.NewRegistryCatalog(registryClient, []platformmcp.CatalogDescriptor{config.CatalogDescriptor()})
	candidates, err := catalog.Search(t.Context(), "")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, ProviderKey, candidates[0].ProviderKey)
	require.Equal(t, CanonicalRef, candidates[0].CatalogRef)
	require.Nil(t, candidates[0].ToolCount)
	require.True(t, candidates[0].SupportsDCR)
	require.Equal(t, "dcr", candidates[0].AuthSetup)

	details, err := catalog.Inspect(t.Context(), ProviderKey, CanonicalRef)
	require.NoError(t, err)
	require.Equal(t, CanonicalRef, details.CatalogRef)
	require.Equal(t, "streamable-http", details.Transport)
	require.Nil(t, details.ToolNames)
	require.Nil(t, details.ToolCount)
	require.True(t, details.SupportsDCR)
	require.Equal(t, "dcr", details.AuthSetup)
}

func TestDynamicRegistryCatalogReloadsServerOwnedDescriptors(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	origin, err := url.Parse(server.URL)
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	server.Config.Handler = NewRegistryHTTP(config).Handler()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	registryPolicy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{},
		guardian.WithTLSRootCAs(roots),
	)
	require.NoError(t, err)
	registryClient := externalmcp.NewRegistryClient(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		registryPolicy,
		registryBackend{},
		cache.NoopCache,
	)

	loads := 0
	catalog := platformmcp.NewDynamicRegistryCatalog(registryClient, func(_ context.Context) ([]platformmcp.CatalogDescriptor, error) {
		loads++
		return []platformmcp.CatalogDescriptor{config.CatalogDescriptor()}, nil
	})

	candidates, err := catalog.Search(t.Context(), "")
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	_, err = catalog.Inspect(t.Context(), ProviderKey, CanonicalRef)
	require.NoError(t, err)
	require.Equal(t, 2, loads, "search and inspect each reload current server-owned registry descriptors")
}

func TestRegistryCatalogSearchFailsClosedWhenAnySourceIsUnavailable(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	origin, err := url.Parse(server.URL)
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	server.Config.Handler = NewRegistryHTTP(config).Handler()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	registryPolicy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{},
		guardian.WithTLSRootCAs(roots),
	)
	require.NoError(t, err)
	registryClient := externalmcp.NewRegistryClient(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		registryPolicy,
		registryBackend{},
		cache.NoopCache,
	)

	unavailable := platformmcp.BrowserCatalogDescriptor(externalmcp.Registry{
		ID:  uuid.New(),
		URL: server.URL + "/unavailable",
	})
	catalog := platformmcp.NewRegistryCatalogSources([]platformmcp.RegistryCatalogSource{
		{Client: registryClient, Descriptors: []platformmcp.CatalogDescriptor{config.CatalogDescriptor()}},
		{Client: registryClient, Descriptors: []platformmcp.CatalogDescriptor{unavailable}},
	})

	candidates, err := catalog.Search(t.Context(), "")
	require.ErrorContains(t, err, "list platform mcp catalog")
	require.Nil(t, candidates, "a failed source must not return a partial reviewed catalogue")
}

func TestRegistryHTTPRejectsUnexpectedRoutesAndListQueries(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	handler := NewRegistryHTTP(config).Handler()

	invalidList := httptest.NewRequest(http.MethodGet, registryListPath+"?version=latest&limit=10", nil)
	invalidListResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidListResponse, invalidList)
	require.Equal(t, http.StatusBadRequest, invalidListResponse.Code)

	unknown := httptest.NewRequest(http.MethodGet, "/v0.1/servers/unknown/versions/latest", nil)
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	require.Equal(t, http.StatusNotFound, unknownResponse.Code)
}

type registryBackend struct{}

func (registryBackend) Match(*http.Request) bool { return false }

func (registryBackend) Authorize(*http.Request) error { return nil }

func TestRegistryEntryUsesNativeAuthAndStandardLifecycleWithoutToolClaims(t *testing.T) {
	t.Parallel()
	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	entry := NewRegistryHTTP(config).entry()
	require.Equal(t, map[string]any{
		"io.modelcontextprotocol.registry/official": map[string]any{"status": "active", "isLatest": true},
		"com.speakeasy.ai/catalog":                  map[string]any{"authSetup": "dcr", "supportsDcr": true},
	}, entry["_meta"])
	raw, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"tools"`)
	require.NotContains(t, string(raw), `"tool_count"`)
	require.NotContains(t, string(raw), `"is_read_only"`)
}

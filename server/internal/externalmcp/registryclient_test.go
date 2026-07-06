package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gentypes "github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type PassthroughBackend struct{}

var _ RegistryBackend = (*PassthroughBackend)(nil)

func (p *PassthroughBackend) Authorize(req *http.Request) error {
	return nil
}

func (p *PassthroughBackend) Match(req *http.Request) bool {
	return false
}

func TestListServers_FiltersDeletedServers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := listResponse{
			Servers: []serverEntry{
				{
					Server: serverJSON{
						Name:        "active-server",
						Description: "An active server",
						Version:     "1.0.0",
					},
					Meta: pulseMCPServerMeta{
						Version: serverMetaVersion{
							Status: "active",
						},
					},
				},
				{
					Server: serverJSON{
						Name:        "deleted-server",
						Description: "A deleted server",
						Version:     "1.0.0",
					},
					Meta: pulseMCPServerMeta{
						Version: serverMetaVersion{
							Status: "deleted",
						},
					},
				},
				{
					Server: serverJSON{
						Name:        "another-active",
						Description: "Another active server",
						Version:     "2.0.0",
					},
					Meta: pulseMCPServerMeta{
						Version: serverMetaVersion{
							Status: "active",
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	result, err := client.ListServers(ctx, registry, ListServersParams{})

	require.NoError(t, err)
	require.Len(t, result.Servers, 2)
	require.Equal(t, "active-server", result.Servers[0].RegistrySpecifier)
	require.Equal(t, "another-active", result.Servers[1].RegistrySpecifier)
}

func TestListServers_OmitsToolsAndComputesScalars(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	inputSchema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := listResponse{
			Servers: []serverEntry{
				{
					Server: serverJSON{
						Name:        "readonly-server",
						Description: "Every tool is read-only",
						Version:     "1.0.0",
					},
					Meta: pulseMCPServerMeta{
						Version: serverMetaVersion{
							Status: "active",
							FirstRemote: serverRemoteMeta{
								Tools: []serverTool{
									{Name: "search", Description: "Search things", InputSchema: inputSchema, Annotations: map[string]any{"readOnlyHint": true}},
									{Name: "list", Description: "List things", InputSchema: inputSchema, Annotations: map[string]any{"readOnlyHint": true}},
								},
							},
						},
					},
				},
				{
					Server: serverJSON{
						Name:        "writer-server",
						Description: "Has a write tool",
						Version:     "1.0.0",
					},
					Meta: pulseMCPServerMeta{
						Version: serverMetaVersion{
							Status: "active",
							FirstRemote: serverRemoteMeta{
								Tools: []serverTool{
									{Name: "read", InputSchema: inputSchema, Annotations: map[string]any{"readOnlyHint": true}},
									{Name: "write", InputSchema: inputSchema},
								},
							},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer srv.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
	client.httpClient = srv.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: srv.URL,
	}

	result, err := client.ListServers(ctx, registry, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, result.Servers, 2)

	// Scalars are precomputed; all tools read-only -> is_read_only true.
	readonly := result.Servers[0]
	require.Equal(t, 2, readonly.ToolCount)
	require.True(t, readonly.IsReadOnly)

	// One non-read-only tool -> is_read_only false.
	writer := result.Servers[1]
	require.Equal(t, 2, writer.ToolCount)
	require.False(t, writer.IsReadOnly)

	// The _meta blob must not carry tool definitions (schemas/descriptions).
	metaJSON, err := json.Marshal(readonly.Meta)
	require.NoError(t, err)
	require.NotContains(t, string(metaJSON), "properties")
	require.NotContains(t, string(metaJSON), "Search things")
}

func TestListServers_PreservesRemoteHeadersAndVariables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	headerDescription := "API key for authentication"
	headerPlaceholder := "Bearer token"
	variableDescription := "Deployment region"
	defaultRegion := "us-east-1"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := listResponse{
			Servers: []serverEntry{
				{
					Server: serverJSON{
						Name:        "templated-server",
						Description: "A server with remote metadata",
						Version:     "1.0.0",
						Remotes: []serverRemoteJSON{
							{
								URL:  "https://api.example.com/{region}/mcp",
								Type: "streamable-http",
								Headers: []RemoteHeader{
									{
										Name:        "Authorization",
										IsSecret:    true,
										IsRequired:  true,
										Description: &headerDescription,
										Placeholder: &headerPlaceholder,
									},
								},
								Variables: map[string]RemoteVariable{
									"region": {
										Description: &variableDescription,
										IsRequired:  true,
										Default:     &defaultRegion,
										Choices:     []string{"us-east-1", "eu-west-1"},
									},
								},
							},
						},
					},
					Meta: pulseMCPServerMeta{
						Version: serverMetaVersion{
							Status: "active",
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	result, err := client.ListServers(ctx, registry, ListServersParams{})

	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	require.Len(t, result.Servers[0].Remotes, 1)

	remote := result.Servers[0].Remotes[0]
	require.Equal(t, "https://api.example.com/{region}/mcp", remote.URL)
	require.Len(t, remote.Headers, 1)
	require.Equal(t, "Authorization", remote.Headers[0].Name)
	require.Equal(t, headerDescription, *remote.Headers[0].Description)
	require.Equal(t, headerPlaceholder, *remote.Headers[0].Placeholder)
	require.NotNil(t, remote.Headers[0].IsRequired)
	require.True(t, *remote.Headers[0].IsRequired)
	require.NotNil(t, remote.Headers[0].IsSecret)
	require.True(t, *remote.Headers[0].IsSecret)

	require.Contains(t, remote.Variables, "region")
	region := remote.Variables["region"]
	require.Equal(t, variableDescription, *region.Description)
	require.Equal(t, defaultRegion, *region.Default)
	require.Equal(t, []string{"us-east-1", "eu-west-1"}, region.Choices)
	require.NotNil(t, region.IsRequired)
	require.True(t, *region.IsRequired)
}

func TestListServers_FetchesAllPagesAndDeduplicates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// The registry returns overlapping pages under a continuing cursor: the
	// second page repeats most of the first. The client must dedupe so the
	// catalog count does not balloon when more servers are loaded.
	secondCursor := "page-2"
	firstPageNames := numberedServerNames("server", 0, 50)
	secondPageNames := append([]string{}, firstPageNames[4:]...)
	secondPageNames = append(secondPageNames, numberedServerNames("server", 50, 56)...)
	var gotCursors []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		gotCursors = append(gotCursors, cursor)

		response := listResponse{}
		switch cursor {
		case "":
			response.Servers = activeServerEntries(firstPageNames)
			response.Metadata.NextCursor = &secondCursor
		case secondCursor:
			response.Servers = activeServerEntries(secondPageNames)
		default:
			t.Fatalf("unexpected upstream cursor: %q", cursor)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	result, err := client.ListServers(ctx, registry, ListServersParams{})
	require.NoError(t, err)

	require.Equal(t, []string{"", secondCursor}, gotCursors)
	require.Equal(t, numberedServerNames("server", 0, 56), registrySpecifiers(result.Servers))
}

func TestListServers_FiltersBySearchInMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	var gotSearchParams []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSearchParams = append(gotSearchParams, r.URL.Query().Get("search"))

		response := listResponse{
			Servers: activeServerEntries([]string{"github-mcp", "slack-mcp", "github-actions"}),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	search := "github"
	result, err := client.ListServers(ctx, registry, ListServersParams{
		Search: &search,
	})
	require.NoError(t, err)

	require.Equal(t, []string{"github-mcp", "github-actions"}, registrySpecifiers(result.Servers))
	// Search is applied in memory and must never be forwarded upstream.
	require.Equal(t, []string{""}, gotSearchParams)
}

func TestListServers_CachesResultAndClearCacheRefetches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	var upstreamCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++

		response := listResponse{
			Servers: activeServerEntries(numberedServerNames("server", 0, 3)),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, newRegistryTestCache())
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	firstResult, err := client.ListServers(ctx, registry, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, firstResult.Servers, 3)
	require.Equal(t, 1, upstreamCalls)

	cachedResult, err := client.ListServers(ctx, registry, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, cachedResult.Servers, 3)
	require.Equal(t, 1, upstreamCalls)

	err = client.ClearCache(ctx, registry.URL)
	require.NoError(t, err)

	rebuiltResult, err := client.ListServers(ctx, registry, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, rebuiltResult.Servers, 3)
	require.Equal(t, 2, upstreamCalls)
}

func TestListServers_CachesPerRegistryIDForSharedURL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	var upstreamCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++

		response := listResponse{
			Servers: activeServerEntries(numberedServerNames("server", 0, 2)),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, newRegistryTestCache())
	client.httpClient = server.Client()

	// Two registries sharing a URL (e.g. one recreated under the same endpoint)
	// must not serve each other's cached servers, because each server carries
	// the registry ID stamped at conversion time.
	registryA := Registry{ID: uuid.New(), URL: server.URL}
	registryB := Registry{ID: uuid.New(), URL: server.URL}

	resultA, err := client.ListServers(ctx, registryA, ListServersParams{})
	require.NoError(t, err)
	require.NotEmpty(t, resultA.Servers)
	for _, s := range resultA.Servers {
		require.NotNil(t, s.RegistryID)
		require.Equal(t, registryA.ID.String(), *s.RegistryID)
	}

	resultB, err := client.ListServers(ctx, registryB, ListServersParams{})
	require.NoError(t, err)
	require.NotEmpty(t, resultB.Servers)
	for _, s := range resultB.Servers {
		require.NotNil(t, s.RegistryID)
		require.Equal(t, registryB.ID.String(), *s.RegistryID)
	}

	// Distinct registry IDs key distinct cache entries, so each crawled upstream.
	require.Equal(t, 2, upstreamCalls)
}

func TestListServers_DoesNotCacheTruncatedCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	pageOneCursor := "page-1"
	pageTwoCursor := "page-2"
	var gotCursors []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		gotCursors = append(gotCursors, cursor)

		response := listResponse{}
		switch cursor {
		case "":
			response.Servers = activeServerEntries(numberedServerNames("server", 0, 50))
			response.Metadata.NextCursor = &pageOneCursor
		case pageOneCursor:
			response.Servers = activeServerEntries(numberedServerNames("server", 50, 100))
			response.Metadata.NextCursor = &pageTwoCursor
		default:
			t.Fatalf("crawl exceeded page bound; unexpected cursor %q", cursor)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, newRegistryTestCache())
	client.httpClient = server.Client()
	registry := Registry{ID: uuid.New(), URL: server.URL}

	// The catalog advertises a third page, exceeding the 2-page (100-server)
	// bound. ListServers serves the truncated 100 so the page still renders, but
	// stops at the bound rather than crawling page 3.
	first, err := client.ListServers(ctx, registry, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, first.Servers, 100)
	require.Equal(t, []string{"", pageOneCursor}, gotCursors)

	// A partial catalog must not be cached: the next call re-crawls upstream
	// rather than serving a frozen partial list for the full 24h TTL.
	second, err := client.ListServers(ctx, registry, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, second.Servers, 100)
	require.Equal(t, []string{"", pageOneCursor, "", pageOneCursor}, gotCursors)
}

func TestGetServerDetails_OnlyStreamableHTTP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v0.1/servers/test-server/versions/latest")

		response := serverDetailsEntry{
			Server: serverDetailsJSON{
				Name:        "test-server",
				Description: "Test server description",
				Version:     "1.0.0",
				Remotes: []serverRemoteJSON{
					{URL: "https://example.com/streamable", Type: "streamable-http"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server", []string{})

	require.NoError(t, err)
	require.NotNil(t, details)
	require.Equal(t, "test-server", details.Name)
	require.Equal(t, "Test server description", details.Description)
	require.Equal(t, "1.0.0", details.Version)
	require.Equal(t, "https://example.com/streamable", details.RemoteURL)
	require.Equal(t, types.TransportTypeStreamableHTTP, details.TransportType)
}

func TestGetServerDetails_OnlySSE(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v0.1/servers/test-server/versions/latest")

		response := serverDetailsEntry{
			Server: serverDetailsJSON{
				Name:        "test-server",
				Description: "Test server description",
				Version:     "1.0.0",
				Remotes: []serverRemoteJSON{
					{URL: "https://example.com/sse", Type: "sse"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server", []string{})

	require.NoError(t, err)
	require.NotNil(t, details)
	require.Equal(t, "test-server", details.Name)
	require.Equal(t, "Test server description", details.Description)
	require.Equal(t, "1.0.0", details.Version)
	require.Equal(t, "https://example.com/sse", details.RemoteURL)
	require.Equal(t, types.TransportTypeSSE, details.TransportType)
}

func TestGetServerDetails_PrefersStreamableHTTPOverSSE(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v0.1/servers/test-server/versions/latest")

		response := serverDetailsEntry{
			Server: serverDetailsJSON{
				Name:        "test-server",
				Description: "Test server description",
				Version:     "1.0.0",
				Remotes: []serverRemoteJSON{
					{URL: "https://example.com/sse", Type: "sse"},
					{URL: "https://example.com/streamable", Type: "streamable-http"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server", []string{})

	require.NoError(t, err)
	require.NotNil(t, details)
	require.Equal(t, "test-server", details.Name)
	require.Equal(t, "Test server description", details.Description)
	require.Equal(t, "1.0.0", details.Version)
	require.Equal(t, "https://example.com/streamable", details.RemoteURL)
	require.Equal(t, types.TransportTypeStreamableHTTP, details.TransportType)
}

func TestGetServerDetails_SelectedRemotesFiltersToSSE(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v0.1/servers/test-server/versions/latest")

		// Server has both SSE and streamable-http remotes
		response := serverDetailsEntry{
			Server: serverDetailsJSON{
				Name:        "test-server",
				Description: "Test server description",
				Version:     "1.0.0",
				Remotes: []serverRemoteJSON{
					{URL: "https://example.com/sse", Type: "sse"},
					{URL: "https://example.com/streamable", Type: "streamable-http"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	// Filter to only allow the SSE remote, not the streamable-http
	selectedRemotes := []string{"https://example.com/sse"}
	details, err := client.GetServerDetails(ctx, registry, "test-server", selectedRemotes)

	require.NoError(t, err)
	require.NotNil(t, details)
	require.Equal(t, "test-server", details.Name)
	// Should select SSE since streamable-http is filtered out
	require.Equal(t, "https://example.com/sse", details.RemoteURL)
	require.Equal(t, types.TransportTypeSSE, details.TransportType)
}

func TestGetServerDetails_SelectedRemotesStillPrefersStreamableHTTP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v0.1/servers/test-server/versions/latest")

		// Server has both SSE and streamable-http remotes
		response := serverDetailsEntry{
			Server: serverDetailsJSON{
				Name:        "test-server",
				Description: "Test server description",
				Version:     "1.0.0",
				Remotes: []serverRemoteJSON{
					{URL: "https://example.com/sse", Type: "sse"},
					{URL: "https://example.com/streamable", Type: "streamable-http"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, cache.NoopCache)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	// Allow both remotes, should still prefer streamable-http
	selectedRemotes := []string{"https://example.com/sse", "https://example.com/streamable"}
	details, err := client.GetServerDetails(ctx, registry, "test-server", selectedRemotes)

	require.NoError(t, err)
	require.NotNil(t, details)
	require.Equal(t, "test-server", details.Name)
	// Should prefer streamable-http when both are allowed
	require.Equal(t, "https://example.com/streamable", details.RemoteURL)
	require.Equal(t, types.TransportTypeStreamableHTTP, details.TransportType)
}

func activeServerEntry(name string) serverEntry {
	return serverEntry{
		Server: serverJSON{
			Name:        name,
			Description: "Test server",
			Version:     "1.0.0",
		},
		Meta: pulseMCPServerMeta{
			Version: serverMetaVersion{
				Status: "active",
			},
		},
	}
}

func activeServerEntries(names []string) []serverEntry {
	entries := make([]serverEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, activeServerEntry(name))
	}
	return entries
}

func numberedServerNames(prefix string, start int, end int) []string {
	names := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		names = append(names, fmt.Sprintf("%s-%02d", prefix, i))
	}
	return names
}

func registrySpecifiers(servers []*gentypes.ExternalMCPServerEntry) []string {
	result := make([]string, 0, len(servers))
	for _, server := range servers {
		result = append(result, server.RegistrySpecifier)
	}
	return result
}

type registryTestCache struct {
	mu   sync.Mutex
	data map[string][]byte
}

var _ cache.Cache = (*registryTestCache)(nil)

func newRegistryTestCache() *registryTestCache {
	return &registryTestCache{
		data: map[string][]byte{},
	}
}

func (c *registryTestCache) Get(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, ok := c.data[key]
	if !ok {
		return errors.New("cache miss")
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("unmarshal %s: %w", key, err)
	}
	return nil
}

func (c *registryTestCache) GetAndDelete(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, ok := c.data[key]
	if !ok {
		return errors.New("cache miss")
	}
	delete(c.data, key)
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("unmarshal %s: %w", key, err)
	}
	return nil
}

func (c *registryTestCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", key, err)
	}
	c.data[key] = raw
	return nil
}

func (c *registryTestCache) Add(_ context.Context, key string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.data[key]; ok {
		return false, nil
	}
	c.data[key] = []byte("1")
	return true, nil
}

func (c *registryTestCache) Update(ctx context.Context, key string, value any) error {
	return c.Set(ctx, key, value, time.Hour)
}

func (c *registryTestCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
	return nil
}

func (c *registryTestCache) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (c *registryTestCache) ListAppend(_ context.Context, _ string, _ any, _ time.Duration) error {
	return errors.New("list append is not supported")
}

func (c *registryTestCache) ListRange(_ context.Context, _ string, _ int64, _ int64, _ any) error {
	return errors.New("list range is not supported")
}

func (c *registryTestCache) DeleteByPrefix(_ context.Context, prefix string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key := range c.data {
		if strings.HasPrefix(key, prefix) {
			delete(c.data, key)
		}
	}
	return nil
}

// TestListServers_ComputesSupportsDcr exercises the raw PulseMCP wire format so
// the JSON tags for detail.authorizationServerMetadata.registration_endpoint
// stay faithful to the upstream schema. A non-empty registration endpoint on
// any remote's OAuth auth option marks the server as DCR-capable; OAuth without
// a registration endpoint and API-key servers are not.
func TestListServers_ComputesSupportsDcr(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// Raw body matching PulseMCP's documented _meta shape.
	body := `{
		"servers": [
			{
				"server": {"name": "dcr-oauth", "description": "OAuth with DCR", "version": "1.0.0"},
				"_meta": {"com.pulsemcp/server-version": {"status": "active", "remotes[0]": {"authOptions": [
					{"type": "oauth", "detail": {"authorizationServerMetadata": {"registration_endpoint": "https://idp.example/oauth/register"}}}
				]}}}
			},
			{
				"server": {"name": "oauth-no-dcr", "description": "OAuth without DCR", "version": "1.0.0"},
				"_meta": {"com.pulsemcp/server-version": {"status": "active", "remotes[0]": {"authOptions": [
					{"type": "oauth", "detail": {"authorizationServerMetadata": {"registration_endpoint": ""}}}
				]}}}
			},
			{
				"server": {"name": "apikey", "description": "API key auth", "version": "1.0.0"},
				"_meta": {"com.pulsemcp/server-version": {"status": "active", "remotes[0]": {"authOptions": [
					{"type": "api_key", "detail": {"sources": [{"location": "header", "name": "Authorization"}]}}
				]}}}
			},
			{
				"server": {"name": "dcr-secondary-remote", "description": "DCR on a non-first remote", "version": "1.0.0"},
				"_meta": {"com.pulsemcp/server-version": {"status": "active", "remotes[1]": {"authOptions": [
					{"type": "oauth", "detail": {"authorizationServerMetadata": {"registration_endpoint": "https://idp.example/oauth/register"}}}
				]}}}
			}
		],
		"metadata": {}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write([]byte(body))
		assert.NoError(t, writeErr)
	}))
	defer srv.Close()

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
	client.httpClient = srv.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: srv.URL,
	}

	result, err := client.ListServers(ctx, registry, ListServersParams{})
	require.NoError(t, err)
	require.Len(t, result.Servers, 4)

	byName := make(map[string]bool, len(result.Servers))
	for _, s := range result.Servers {
		byName[s.RegistrySpecifier] = s.SupportsDcr
	}

	require.True(t, byName["dcr-oauth"], "oauth with a registration endpoint supports DCR")
	require.False(t, byName["oauth-no-dcr"], "oauth without a registration endpoint does not support DCR")
	require.False(t, byName["apikey"], "api-key servers do not support DCR")
	require.True(t, byName["dcr-secondary-remote"], "a registration endpoint on any remote slot counts")
}

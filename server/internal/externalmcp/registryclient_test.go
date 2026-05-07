package externalmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
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

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
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

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server", nil)

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

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server", nil)

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

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server", nil)

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

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
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

	client := NewRegistryClient(logger, tracerProvider, guardianPolicy, &PassthroughBackend{}, nil)
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

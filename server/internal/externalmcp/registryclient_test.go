package externalmcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	tracernoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type PassthroughBackend struct{}

var _ RegistryBackend = (*PassthroughBackend)(nil)

func (p *PassthroughBackend) Authorize(req *http.Request) error {
	return nil
}

func (p *PassthroughBackend) Match(req *http.Request) bool {
	return false
}

func TestGetServerDetails_OnlyStreamableHTTP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v0.1/servers/test-server/versions/latest")

		response := getServerResponse{
			Server: serverJSON{
				Name:        "test-server",
				Description: "Test server description",
				Version:     "1.0.0",
				Remotes: []serverRemoteMeta{
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

	client := NewRegistryClient(logger, tracernoop.NewTracerProvider(), &PassthroughBackend{})
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server")

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
	logger := slog.New(slog.DiscardHandler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v0.1/servers/test-server/versions/latest")

		response := getServerResponse{
			Server: serverJSON{
				Name:        "test-server",
				Description: "Test server description",
				Version:     "1.0.0",
				Remotes: []serverRemoteMeta{
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

	client := NewRegistryClient(logger, tracernoop.NewTracerProvider(), &PassthroughBackend{})
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server")

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
	logger := slog.New(slog.DiscardHandler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v0.1/servers/test-server/versions/latest")

		response := getServerResponse{
			Server: serverJSON{
				Name:        "test-server",
				Description: "Test server description",
				Version:     "1.0.0",
				Remotes: []serverRemoteMeta{
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

	client := NewRegistryClient(logger, tracernoop.NewTracerProvider(), &PassthroughBackend{})
	client.httpClient = server.Client()
	registry := Registry{
		ID:  uuid.New(),
		URL: server.URL,
	}

	details, err := client.GetServerDetails(ctx, registry, "test-server")

	require.NoError(t, err)
	require.NotNil(t, details)
	require.Equal(t, "test-server", details.Name)
	require.Equal(t, "Test server description", details.Description)
	require.Equal(t, "1.0.0", details.Version)
	require.Equal(t, "https://example.com/streamable", details.RemoteURL)
	require.Equal(t, types.TransportTypeStreamableHTTP, details.TransportType)
}

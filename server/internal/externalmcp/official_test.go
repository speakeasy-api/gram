package externalmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newOfficialRegistryAdapter(t *testing.T) *OfficialRegistryAdapter {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return NewOfficialRegistryAdapter(testenv.NewLogger(t), policy)
}

func TestOfficialRegistryAdapterListServersPaginatesAndFiltersDeleted(t *testing.T) {
	t.Parallel()

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.1/servers" || r.URL.Query().Get("version") != "latest" || r.URL.Query().Get("limit") != "50" || r.URL.Query().Get("search") != "github" {
			http.Error(w, "unexpected list request", http.StatusBadRequest)
			return
		}
		requests = append(requests, r.URL.Query().Get("cursor"))

		page := officialListResponse{}
		switch r.URL.Query().Get("cursor") {
		case "":
			page.Servers = []officialServerEntry{
				officialTestEntry("alpha", "active"),
				officialTestEntry("removed", "deleted"),
			}
			next := "second-page"
			page.Metadata.NextCursor = &next
		case "second-page":
			page.Servers = []officialServerEntry{
				officialTestEntry("alpha", "active"),
				officialTestEntry("beta", "active"),
			}
		default:
			http.Error(w, "unexpected cursor", http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Errorf("encode official registry list response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	search := "github"
	result, err := newOfficialRegistryAdapter(t).ListServers(t.Context(), Registry{ID: uuid.New(), URL: server.URL}, ListServersParams{Search: &search})
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta"}, registrySpecifiers(result.Servers))
	require.Equal(t, []string{"", "second-page"}, requests)
}

func TestOfficialRegistryAdapterListServersBoundsPagination(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		page := officialListResponse{Servers: []officialServerEntry{officialTestEntry("server-"+strconv.Itoa(requests), "active")}}
		next := "next-" + strconv.Itoa(requests)
		page.Metadata.NextCursor = &next
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Errorf("encode bounded official registry list response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	_, err := newOfficialRegistryAdapter(t).ListServers(t.Context(), Registry{ID: uuid.New(), URL: server.URL}, ListServersParams{})
	require.ErrorContains(t, err, "exceeded 10-page catalogue bound")
	require.Equal(t, officialRegistryMaxPages, requests)
}

func TestOfficialRegistryAdapterGetServerDetailsPrefersAllowedStreamableHTTP(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.1/servers/example/versions/latest" {
			http.Error(w, "unexpected detail request", http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(struct {
			Server serverJSON `json:"server"`
		}{Server: serverJSON{
			Name:        "example",
			Description: "Example",
			Version:     "1.0.0",
			Remotes: []serverRemoteJSON{
				{URL: "https://example.test/events", Type: "sse"},
				{URL: "https://example.test/mcp", Type: "streamable-http"},
			},
		}}); err != nil {
			t.Errorf("encode official registry detail response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	details, err := newOfficialRegistryAdapter(t).GetServerDetails(
		context.Background(),
		Registry{ID: uuid.New(), URL: server.URL},
		"example",
		[]string{"https://example.test/events", "https://example.test/mcp"},
	)
	require.NoError(t, err)
	require.Equal(t, "https://example.test/mcp", details.RemoteURL)
	require.Equal(t, externalmcptypes.TransportTypeStreamableHTTP, details.TransportType)
}

func officialTestEntry(name, status string) officialServerEntry {
	entry := officialServerEntry{Server: serverJSON{Name: name, Version: "1.0.0"}}
	entry.Meta.Official.Status = status
	return entry
}

package advisories_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/advisories"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
)

const osvBody = `{
  "vulns": [
    {"id": "GHSA-aaaa-1111", "summary": "prototype pollution", "published": "2024-02-01T00:00:00Z",
     "database_specific": {"severity": "HIGH"}},
    {"id": "GHSA-bbbb-2222", "summary": "command injection", "published": "2026-01-15T00:00:00Z",
     "database_specific": {"severity": "CRITICAL"}}
  ]
}`

func serve(t *testing.T, body string) (*httptest.Server, func() map[string]any) {
	t.Helper()

	var mu sync.Mutex
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(raw, &request)
		mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server, func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return request
	}
}

func TestQuery(t *testing.T) {
	t.Parallel()

	server, sentQuery := serve(t, osvBody)
	client := advisories.NewClient(server.Client(), advisories.WithBaseURL(server.URL))

	got, err := client.Query(t.Context(), identity.RegistryNPM, "@scope/mcp-server", "")
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, "npm", got.Ecosystem)
	require.Equal(t, "@scope/mcp-server", got.Package)
	require.Equal(t, 2, got.KnownCount)
	require.Len(t, got.Advisories, 2)

	// Newest first.
	require.Equal(t, "GHSA-bbbb-2222", got.Advisories[0].ID)
	require.Equal(t, "CRITICAL", got.Advisories[0].Severity)
	require.Equal(t, "GHSA-aaaa-1111", got.Advisories[1].ID)

	sent := sentQuery()
	pkg, ok := sent["package"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "npm", pkg["ecosystem"])
	require.Equal(t, "@scope/mcp-server", pkg["name"])
}

// OSV answers an empty document for a package it has nothing on: checked and
// clean, a real report rather than an absence.
func TestQuery_CleanPackage(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, `{}`)
	client := advisories.NewClient(server.Client(), advisories.WithBaseURL(server.URL))

	got, err := client.Query(t.Context(), identity.RegistryPyPI, "mcp-server", "")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "PyPI", got.Ecosystem)
	require.Equal(t, 0, got.KnownCount)
	require.Empty(t, got.Advisories)
}

// A registry OSV does not cover is not consulted.
func TestQuery_UncoveredRegistry(t *testing.T) {
	t.Parallel()

	client := advisories.NewClient(http.DefaultClient)

	got, err := client.Query(t.Context(), identity.Registry(""), "anything", "")
	require.NoError(t, err)
	require.Nil(t, got)
}

// A failing database is an error the assembler records as a gap — never a
// clean report.
func TestQuery_FailureIsAnError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	client := advisories.NewClient(server.Client(), advisories.WithBaseURL(server.URL))

	_, err := client.Query(t.Context(), identity.RegistryNPM, "mcp-server", "")
	require.Error(t, err)
}

// The stored sample stays bounded while the count reflects everything.
func TestQuery_SampleIsBounded(t *testing.T) {
	t.Parallel()

	vulns := make([]map[string]any, 0, 25)
	for i := range 25 {
		vulns = append(vulns, map[string]any{
			"id":        string(rune('a'+i)) + "-advisory",
			"published": "2024-01-01T00:00:00Z",
		})
	}
	body, err := json.Marshal(map[string]any{"vulns": vulns})
	require.NoError(t, err)

	server, _ := serve(t, string(body))
	client := advisories.NewClient(server.Client(), advisories.WithBaseURL(server.URL))

	got, err := client.Query(t.Context(), identity.RegistryNPM, "mcp-server", "")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 25, got.KnownCount)
	require.Len(t, got.Advisories, 10)
}

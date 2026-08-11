package repometa_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repometa"
)

const repoBody = `{
  "full_name": "acme/mcp-server",
  "stargazers_count": 412,
  "forks_count": 37,
  "open_issues_count": 9,
  "archived": false,
  "created_at": "2023-05-01T10:00:00Z",
  "pushed_at": "2026-07-30T18:30:00Z"
}`

// serve answers /repos/{owner}/{repo} with the repo document and the
// contributors probe with a one-item page whose Link header names the last
// page, which is how the contributor count travels.
func serve(t *testing.T, contributorsLink string) (*httptest.Server, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()

		if r.URL.Path == "/repos/acme/mcp-server/contributors" {
			if contributorsLink != "" {
				w.Header().Set("Link", contributorsLink)
			}
			_, _ = w.Write([]byte(`[{"login": "alice"}]`))
			return
		}
		_, _ = w.Write([]byte(repoBody))
	}))
	t.Cleanup(server.Close)

	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), paths...)
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, `<https://api.github.com/repos/acme/mcp-server/contributors?per_page=1&anon=false&page=23>; rel="last"`)
	client := repometa.NewClient(server.Client(), repometa.WithBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), "https://github.com/acme/mcp-server")
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, "github.com", got.Host)
	require.Equal(t, "acme", got.Owner)
	require.Equal(t, "mcp-server", got.Name)
	require.Equal(t, 412, got.Stars)
	require.Equal(t, 37, got.Forks)
	require.Equal(t, 9, got.OpenIssues)
	require.False(t, got.Archived)
	require.Equal(t, 2023, got.CreatedAt.Year())
	require.Equal(t, 2026, got.PushedAt.Year())
	require.Equal(t, 23, got.ContributorCount)
}

// A single page of contributors carries no Link header: the page length is
// the count.
func TestLookup_SingleContributorPage(t *testing.T) {
	t.Parallel()

	server, _ := serve(t, "")
	client := repometa.NewClient(server.Client(), repometa.WithBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), "https://github.com/acme/mcp-server")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 1, got.ContributorCount)
}

// A publisher pointing at a repository the host does not know is an ordinary
// outcome, not an error.
func TestLookup_MissingRepository(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	client := repometa.NewClient(server.Client(), repometa.WithBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), "https://github.com/acme/gone")
	require.NoError(t, err)
	require.Nil(t, got)
}

// A rate-limited or failing host is an error the assembler records as a gap.
func TestLookup_RateLimitedIsAnError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	client := repometa.NewClient(server.Client(), repometa.WithBaseURL(server.URL))

	_, err := client.Lookup(t.Context(), "https://github.com/acme/mcp-server")
	require.Error(t, err)
}

// A contributor-count failure leaves the count unknown without failing the
// lookup that already succeeded.
func TestLookup_ContributorFailureIsBestEffort(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/mcp-server/contributors" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(repoBody))
	}))
	t.Cleanup(server.Close)
	client := repometa.NewClient(server.Client(), repometa.WithBaseURL(server.URL))

	got, err := client.Lookup(t.Context(), "https://github.com/acme/mcp-server")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 412, got.Stars)
	require.Equal(t, 0, got.ContributorCount)
}

// An unsupported host is not consulted at all.
func TestLookup_UnsupportedHost(t *testing.T) {
	t.Parallel()

	client := repometa.NewClient(http.DefaultClient)

	got, err := client.Lookup(t.Context(), "https://gitlab.com/acme/mcp-server")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestParseGitHubRepo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw   string
		owner string
		name  string
		ok    bool
	}{
		{"https://github.com/acme/mcp-server", "acme", "mcp-server", true},
		{"git+https://github.com/acme/mcp-server.git", "acme", "mcp-server", true},
		{"git://github.com/acme/mcp-server.git", "acme", "mcp-server", true},
		{"ssh://git@github.com/acme/mcp-server.git", "acme", "mcp-server", true},
		{"git@github.com:acme/mcp-server.git", "acme", "mcp-server", true},
		{"github:acme/mcp-server", "acme", "mcp-server", true},
		{"https://github.com/acme/monorepo/tree/main/packages/server", "acme", "monorepo", true},
		{"https://gitlab.com/acme/mcp-server", "", "", false},
		{"https://github.com/acme", "", "", false},
		{"", "", "", false},
		{"not a url", "", "", false},
	}

	for _, tc := range cases {
		owner, name, ok := repometa.ParseGitHubRepo(tc.raw)
		require.Equal(t, tc.ok, ok, tc.raw)
		require.Equal(t, tc.owner, owner, tc.raw)
		require.Equal(t, tc.name, name, tc.raw)
	}
}

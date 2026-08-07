package docs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// fixtureSitemap mirrors the shape of speakeasy.com/sitemap-0.xml: absolute
// production URLs with trailing slashes, mixing in-scope docs pages with the
// rest of the site. It also includes a near-miss sibling
// (/docs/ai-control-plane-overview) and a duplicate entry, both of which the
// index must reject.
const fixtureSitemap = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://www.speakeasy.com/</loc></url>
  <url><loc>https://www.speakeasy.com/docs/sdks/create-client-sdks/</loc></url>
  <url><loc>https://www.speakeasy.com/docs/ai-control-plane-overview/</loc></url>
  <url><loc>https://www.speakeasy.com/docs/ai-control-plane/observe/tool-logs/</loc></url>
  <url><loc>https://www.speakeasy.com/docs/ai-control-plane/</loc></url>
  <url><loc>https://www.speakeasy.com/docs/ai-control-plane/guides/</loc></url>
  <url><loc>https://www.speakeasy.com/docs/ai-control-plane/guides/github/</loc></url>
  <url><loc>https://www.speakeasy.com/docs/ai-control-plane/observe/tool-logs/</loc></url>
  <url><loc>https://www.speakeasy.com/blog/some-post/</loc></url>
</urlset>`

// newSitemapServer serves fixtureSitemap and counts how many times it was
// fetched, so tests can assert on caching and coalescing.
func newSitemapServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var fetches atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sitemapPath {
			http.NotFound(w, r)
			return
		}
		fetches.Add(1)
		_, _ = w.Write([]byte(fixtureSitemap))
	}))
	t.Cleanup(server.Close)

	return server, &fetches
}

func TestClientIndexFiltersToScope(t *testing.T) {
	t.Parallel()

	server, _ := newSitemapServer(t)
	client := NewClient(server.Client(), server.URL)

	pages, err := client.Index(t.Context())
	require.NoError(t, err)

	paths := make([]string, 0, len(pages))
	for _, page := range pages {
		paths = append(paths, page.Path)
	}

	// Sorted by path, trailing slashes stripped, duplicate collapsed, and both
	// the unrelated site pages and the /docs/ai-control-plane-overview
	// near-miss excluded.
	require.Equal(t, []string{
		"/docs/ai-control-plane",
		"/docs/ai-control-plane/guides",
		"/docs/ai-control-plane/guides/github",
		"/docs/ai-control-plane/observe/tool-logs",
	}, paths)
}

func TestClientIndexDerivesTitleSectionAndURL(t *testing.T) {
	t.Parallel()

	server, _ := newSitemapServer(t)
	client := NewClient(server.Client(), server.URL)

	pages, err := client.Index(t.Context())
	require.NoError(t, err)

	byPath := make(map[string]Page, len(pages))
	for _, page := range pages {
		byPath[page.Path] = page
	}

	root := byPath["/docs/ai-control-plane"]
	require.Equal(t, "AI control plane", root.Title)
	require.Empty(t, root.Section)

	leaf := byPath["/docs/ai-control-plane/observe/tool-logs"]
	require.Equal(t, "Tool logs", leaf.Title)
	require.Equal(t, "observe", leaf.Section)
	// The permalink is built from the client's origin, not the sitemap's, so
	// it stays correct under test and behind any future origin change.
	require.Equal(t, server.URL+"/docs/ai-control-plane/observe/tool-logs", leaf.URL)
}

func TestClientIndexCachesAndCoalesces(t *testing.T) {
	t.Parallel()

	server, fetches := newSitemapServer(t)
	client := NewClient(server.Client(), server.URL)

	// Collect rather than assert inside the goroutines: require.NoError calls
	// FailNow, which is only valid on the test goroutine.
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = client.Index(t.Context())
		})
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}

	_, err := client.Index(t.Context())
	require.NoError(t, err)

	require.Equal(t, int64(1), fetches.Load(), "concurrent and subsequent calls must share one sitemap fetch")
}

func TestClientIndexServesStaleAfterRefreshFailure(t *testing.T) {
	t.Parallel()

	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(fixtureSitemap))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	warm, err := client.Index(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, warm)

	// Expire the cache and break the origin: a stale index beats a broken tool.
	client.mu.Lock()
	client.fetchedAt = client.fetchedAt.Add(-2 * indexCacheTTL)
	client.mu.Unlock()
	fail.Store(true)

	stale, err := client.Index(t.Context())
	require.NoError(t, err)
	require.Equal(t, warm, stale)
}

func TestClientIndexErrorsWhenNoPagesInScope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://www.speakeasy.com/blog/some-post/</loc></url>
</urlset>`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	_, err := client.Index(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pages under /docs/ai-control-plane")
}

func TestClientContentFetchesMarkdown(t *testing.T) {
	t.Parallel()

	// Record the requested path and assert on it after the call: asserting
	// inside the handler would call FailNow off the test goroutine.
	var requestedPath atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Tool Logs\n\nThe raw execution log.\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	content, err := client.Content(t.Context(), "/docs/ai-control-plane/observe/tool-logs")
	require.NoError(t, err)
	require.Equal(t, "# Tool Logs\n\nThe raw execution log.", content)
	// The ".md" suffix is what makes the docs site return markdown at all.
	require.Equal(t, "/docs/ai-control-plane/observe/tool-logs.md", requestedPath.Load())
}

func TestClientContentRejectsNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The docs site labels its HTML error shell as markdown, so the status
		// code is the only trustworthy signal.
		w.Header().Set("Content-Type", "text/markdown")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Not found</body></html>"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	_, err := client.Content(t.Context(), "/docs/ai-control-plane/observe/tool-logs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected status 404")
}

func TestClientContentRejectsOversizedPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Big\n\n" + strings.Repeat("x", maxPageBytes)))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	// Truncating would hand the model a partial page it cannot tell is
	// partial, so an over-cap response must fail loudly instead.
	_, err := client.Content(t.Context(), "/docs/ai-control-plane/observe/tool-logs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds the")
}

func TestClientContentRejectsHTMLServedAsMarkdown(t *testing.T) {
	t.Parallel()

	// A 200 carrying the HTML shell is the failure the status check cannot
	// catch; the body sniff must.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("<!DOCTYPE html>\n<html><body>Error page</body></html>"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	_, err := client.Content(t.Context(), "/docs/ai-control-plane/observe/tool-logs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "was HTML, not markdown")
}

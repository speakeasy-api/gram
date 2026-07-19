package changelog

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// unorderedFeed lists releases out of chronological order and includes an
// unsupported product area (`cli`) with the newest date, exercising both the
// product filter and the newest-first (version-descending on same-day ties)
// sort.
const unorderedFeed = `[
  {"slug":"a","content":"### Old release\n\nOld one.","metadata":{"version":"0.88.0","date":"2026-07-10T00:00:00.000Z","product":"platform"}},
  {"slug":"b","content":"### CLI release\n\nCLI note.","metadata":{"version":"2.0.0","date":"2026-07-20T00:00:00.000Z","product":"cli"}},
  {"slug":"c","content":"### Newer patch\n\nPatch.","metadata":{"version":"0.90.1","date":"2026-07-17T00:00:00.000Z","product":"platform"}},
  {"slug":"d","content":"### Same day minor\n\nMinor.","metadata":{"version":"0.90.0","date":"2026-07-17T00:00:00.000Z","product":"dashboard"}}
]`

// fixtureFeed mirrors the shape of speakeasy.com/changelog/data/gram.json:
// an array of posts with raw markdown content (leading "### " title, prose
// summary paragraph, "#### " itemized sections) and version/date/product
// metadata.
const fixtureFeed = `[
  {
    "slug": "v0-99-0",
    "content": "### Assistants learn about the changelog\n\nThe managed assistant can now read release notes.\n\n#### Features\n\n- **Changelog tool** [#1](https://github.com/example/pull/1) - Adds a platform tool.\n- **Second item** - Another feature.\n\n#### Bug Fixes\n\n- **Fixed a thing** - It works now.",
    "metadata": {
      "version": "0.99.0",
      "date": "2026-07-17T00:00:00.000Z",
      "product": "platform",
      "author": {"name": "Someone"},
      "github_url": "https://github.com/example/releases/tag/server%400.99.0"
    }
  },
  {
    "slug": "v1-50-0",
    "content": "### Dashboard release\n\nDashboard summary text.",
    "metadata": {
      "version": "1.50.0",
      "date": "2026-07-15T00:00:00.000Z",
      "product": "dashboard",
      "author": {"name": "Someone"},
      "github_url": "https://github.com/example/releases/tag/%40gram-ai%2Felements%401.50.0"
    }
  }
]`

func TestClientEntriesParsesFeed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixtureFeed))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	entries, err := client.Entries(t.Context())
	require.NoError(t, err)
	require.Len(t, entries, 2)

	first := entries[0]
	require.Equal(t, "v0.99.0", first.Version)
	require.Equal(t, "platform", first.Product)
	require.Equal(t, "2026-07-17", first.Date)
	require.Equal(t, "Assistants learn about the changelog", first.Title)
	require.Equal(t, "The managed assistant can now read release notes.", first.Summary)
	require.Equal(t, "https://www.speakeasy.com/changelog/release/0.99.0?product=mcp-platform", first.URL)
	require.Contains(t, first.Details, "#### Features")
	require.Contains(t, first.Details, "- **Changelog tool** [#1](https://github.com/example/pull/1) - Adds a platform tool.")
	require.Contains(t, first.Details, "#### Bug Fixes")
	require.NotContains(t, first.Details, "### Assistants learn about the changelog")

	second := entries[1]
	require.Equal(t, "v1.50.0", second.Version)
	require.Equal(t, "dashboard", second.Product)
	require.Equal(t, "2026-07-15", second.Date)
	require.Equal(t, "Dashboard release", second.Title)
	require.Equal(t, "Dashboard summary text.", second.Summary)
	require.Empty(t, second.Details)
}

func TestGetChangelogToolCallDefaults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixtureFeed))
	}))
	t.Cleanup(server.Close)

	tool := NewGetChangelogToolWithURL(server.Client(), server.URL)

	var out bytes.Buffer
	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{}`), &out)
	require.NoError(t, err)

	var result struct {
		Entries   []Entry `json:"entries"`
		SourceURL string  `json:"source_url"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, server.URL, result.SourceURL)
	require.Len(t, result.Entries, 2)
	require.Equal(t, "v0.99.0", result.Entries[0].Version)
	// Details are opt-in to keep default responses small.
	require.Empty(t, result.Entries[0].Details)
	require.NotEmpty(t, result.Entries[0].Summary)
}

func TestGetChangelogToolCallProductFilterAndDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixtureFeed))
	}))
	t.Cleanup(server.Close)

	tool := NewGetChangelogToolWithURL(server.Client(), server.URL)

	var out bytes.Buffer
	payload := `{"product": "platform", "include_details": true, "limit": 1}`
	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(payload), &out)
	require.NoError(t, err)

	var result struct {
		Entries []Entry `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Entries, 1)
	require.Equal(t, "v0.99.0", result.Entries[0].Version)
	require.Equal(t, "platform", result.Entries[0].Product)
	require.Contains(t, result.Entries[0].Details, "#### Features")
}

func TestClientServesStaleEntriesOnFetchError(t *testing.T) {
	t.Parallel()

	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(fixtureFeed))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	entries, err := client.Entries(t.Context())
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Expire the cache and make the origin fail: stale entries should be
	// served instead of surfacing the fetch error.
	failing.Store(true)
	client.mu.Lock()
	client.fetchedAt = time.Now().Add(-time.Hour)
	client.mu.Unlock()

	entries, err = client.Entries(t.Context())
	require.NoError(t, err)
	require.Len(t, entries, 2)
}

func TestClientErrorsWhenNoCacheAndFetchFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	_, err := client.Entries(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected status 500")
}

func TestClientFiltersAndSortsEntries(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(unorderedFeed))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL)

	entries, err := client.Entries(t.Context())
	require.NoError(t, err)

	// The `cli` entry is dropped even though it has the newest date, and the
	// rest are newest-first with the same-day 0.90.1 ahead of 0.90.0.
	require.Len(t, entries, 3)
	require.Equal(t, []string{"v0.90.1", "v0.90.0", "v0.88.0"}, []string{
		entries[0].Version, entries[1].Version, entries[2].Version,
	})
	for _, e := range entries {
		require.NotEqual(t, "v2.0.0", e.Version)
		require.Contains(t, supportedProducts, e.Product)
	}
}

func TestGetChangelogToolCallRejectsUnsupportedProduct(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixtureFeed))
	}))
	t.Cleanup(server.Close)

	tool := NewGetChangelogToolWithURL(server.Client(), server.URL)

	var out bytes.Buffer
	err := tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader(`{"product": "cli"}`), &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported product")
}

func TestClientServesStaleWhenRefreshStalls(t *testing.T) {
	t.Parallel()

	var stall atomic.Bool
	block := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if stall.Load() {
			<-block // simulate a stalled upstream that never responds
			return
		}
		_, _ = w.Write([]byte(fixtureFeed))
	}))
	// Unblock the stalled handler before the server closes so the outstanding
	// request drains (cleanups run last-registered-first).
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(block) })

	client := NewClient(server.Client(), server.URL)

	entries, err := client.Entries(t.Context())
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Expire the cache and make the origin stall indefinitely.
	stall.Store(true)
	client.mu.Lock()
	client.fetchedAt = time.Now().Add(-time.Hour)
	client.mu.Unlock()

	// A caller with a short deadline gets stale entries promptly instead of
	// blocking on the stalled refresh — the fetch no longer holds the mutex.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	entries, err = client.Entries(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestGetChangelogDescriptor(t *testing.T) {
	t.Parallel()

	tool := NewGetChangelogTool(&http.Client{})
	descriptor := tool.Descriptor()

	require.Equal(t, "platform_get_changelog", descriptor.Name)
	require.Equal(t, "changelog", descriptor.SourceSlug)
	require.Equal(t, "get_changelog", descriptor.HandlerName)
	require.NotEmpty(t, descriptor.Description)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, properties, "limit")
	require.Contains(t, properties, "product")
	require.Contains(t, properties, "include_details")
	require.NotNil(t, descriptor.Annotations)
	require.NotNil(t, descriptor.Annotations.ReadOnlyHint)
	require.True(t, *descriptor.Annotations.ReadOnlyHint)
}

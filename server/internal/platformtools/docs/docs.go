// Package docs exposes the public Speakeasy AI control plane documentation
// (speakeasy.com/docs/ai-control-plane) as read-only platform tools for the
// project's managed assistant.
//
// The docs site publishes a markdown counterpart for every page — append
// ".md" to the page path — so retrieval is a deliberate two-step flow rather
// than a general web fetch: platform_list_docs hands the model the page index
// built from the site's sitemap, and platform_get_doc returns one page's
// markdown. platform_get_doc only accepts a path that appears in that index,
// so the tool cannot be steered into fetching an arbitrary URL.
//
// Only pages under /docs/ai-control-plane are indexed. The rest of the docs
// site (SDK generation, Terraform, CLI) describes products the managed
// assistant does not operate, and including it would dilute the index the
// model chooses from.
package docs

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// DefaultSiteURL is the production docs origin. Both the sitemap and the
// per-page markdown are served from it.
const DefaultSiteURL = "https://www.speakeasy.com"

// PathPrefix bounds the indexed corpus. Every indexed page path is either this
// exact value (the section landing page) or a descendant of it.
const PathPrefix = "/docs/ai-control-plane"

// FetchTimeout bounds a single sitemap or page fetch (dial through response
// read) so a stalled marketing-site response can't hang a tool call. Applied
// both to the detached refresh context inside the client and to the dedicated
// guardian client at the call site.
const FetchTimeout = 15 * time.Second

// sitemapPath is the site's only sitemap leaf; /sitemap.xml is an index that
// points at this file. There is no docs-scoped sitemap, so the full URL set is
// fetched and filtered to PathPrefix.
const sitemapPath = "/sitemap-0.xml"

// refreshKey is the singleflight key coalescing concurrent index refreshes.
const refreshKey = "index"

const (
	// indexCacheTTL bounds how often the sitemap is re-fetched. Page *content*
	// changes far more often than the set of page *paths*, and only the paths
	// are cached here, so an hour costs nothing in freshness.
	indexCacheTTL = time.Hour

	// maxSitemapBytes caps how much of the sitemap is read. The live file is
	// ~166KB; 16MB leaves room for years of growth without letting a
	// misbehaving response exhaust memory.
	maxSitemapBytes = 16 << 20

	// maxPageBytes caps a single page fetch. Docs pages run 1-12KB, so a
	// response over 1MB is not a documentation page at all. Exceeding it is an
	// error rather than a truncation — see Content.
	maxPageBytes = 1 << 20

	// minMeaningfulPageLen is the body length below which a page is treated as
	// a stub. Several section landing pages (e.g. .../guides) render their
	// child links client-side and so have an essentially empty markdown
	// counterpart — /docs/ai-control-plane/guides.md is 11 bytes. Returning
	// that alone reads as "these docs are empty", so the tool substitutes the
	// section's child pages instead.
	minMeaningfulPageLen = 200
)

// Page is one documentation page in the index.
type Page struct {
	// Path is the site path, e.g. "/docs/ai-control-plane/observe/tool-logs".
	// It is what platform_get_doc accepts, so the model can pass a list result
	// straight back without transforming it.
	Path string `json:"path"`
	// Title is derived from the final path segment. The site's sitemap carries
	// no titles, and fetching 100+ pages to read their H1 would trade a large
	// request burst for a marginally nicer label — the page's real H1 comes
	// back with its content anyway.
	Title string `json:"title"`
	// Section is the top-level grouping under PathPrefix ("connect",
	// "observe", "guides", ...), empty for the landing page itself.
	Section string `json:"section,omitempty"`
	// URL is the human-facing permalink, for citing sources back to the user.
	URL string `json:"url"`
}

// sitemapURLSet mirrors the sitemaps.org urlset schema, narrowed to the one
// element this package needs.
type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

// Client fetches and caches the docs page index and fetches page markdown.
// Safe for concurrent use: the cache is guarded by a mutex held only for
// in-memory reads/writes, and network I/O happens outside it, coalesced by
// singleflight so a burst of tool calls triggers a single sitemap fetch rather
// than hammering the marketing site.
type Client struct {
	httpClient *guardian.HTTPClient
	siteURL    string
	refresh    singleflight.Group

	mu        sync.Mutex
	pages     []Page
	fetchedAt time.Time
}

// NewClient returns a client reading the sitemap and page markdown from
// siteURL (use DefaultSiteURL in production wiring) through httpClient.
func NewClient(httpClient *guardian.HTTPClient, siteURL string) *Client {
	return &Client{
		httpClient: httpClient,
		siteURL:    strings.TrimSuffix(siteURL, "/"),
		refresh:    singleflight.Group{},
		mu:         sync.Mutex{},
		pages:      nil,
		fetchedAt:  time.Time{},
	}
}

// SiteURL returns the origin this client reads from, for reporting the source
// back to the model alongside results.
func (c *Client) SiteURL() string {
	return c.siteURL
}

// Index returns the documentation page index, refreshing the cache when stale.
// When a refresh fails but a previous fetch succeeded, the stale index is
// returned instead of the error so transient marketing-site outages degrade
// freshness rather than break the tool.
func (c *Client) Index(ctx context.Context) ([]Page, error) {
	c.mu.Lock()
	cached := c.pages
	fresh := cached != nil && time.Since(c.fetchedAt) < indexCacheTTL
	c.mu.Unlock()

	if fresh {
		return cached, nil
	}

	// Refresh outside the cache mutex so a slow fetch never blocks other
	// callers on c.mu. singleflight coalesces a burst of tool calls into one
	// fetch, which runs on a context detached from any single caller (but
	// bounded by FetchTimeout) so one canceled request can't abort the shared
	// refresh. Each caller still honors its own deadline via the select below,
	// falling back to a stale index rather than waiting on the network.
	ch := c.refresh.DoChan(refreshKey, func() (any, error) {
		// A caller that saw a stale cache may land here just after a
		// concurrent flight refreshed it; recheck before paying for a fetch.
		c.mu.Lock()
		if c.pages != nil && time.Since(c.fetchedAt) < indexCacheTTL {
			pages := c.pages
			c.mu.Unlock()
			return pages, nil
		}
		c.mu.Unlock()

		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), FetchTimeout)
		defer cancel()

		pages, err := c.fetchIndex(fetchCtx)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		c.pages = pages
		c.fetchedAt = time.Now()
		c.mu.Unlock()

		return pages, nil
	})

	select {
	case <-ctx.Done():
		if cached != nil {
			return cached, nil
		}
		return nil, fmt.Errorf("await docs index refresh: %w", ctx.Err())
	case res := <-ch:
		if res.Err != nil {
			if cached != nil {
				return cached, nil
			}
			return nil, res.Err
		}
		pages, ok := res.Val.([]Page)
		if !ok {
			return nil, fmt.Errorf("docs index refresh returned unexpected type %T", res.Val)
		}
		return pages, nil
	}
}

func (c *Client) fetchIndex(ctx context.Context) ([]Page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.siteURL+sitemapPath, nil)
	if err != nil {
		return nil, fmt.Errorf("build docs sitemap request: %w", err)
	}
	req.Header.Set("Accept", "application/xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch docs sitemap: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch docs sitemap: unexpected status %d", resp.StatusCode)
	}

	var urlset sitemapURLSet
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxSitemapBytes)).Decode(&urlset); err != nil {
		return nil, fmt.Errorf("decode docs sitemap: %w", err)
	}

	seen := make(map[string]bool, len(urlset.URLs))
	pages := make([]Page, 0, len(urlset.URLs))
	for _, entry := range urlset.URLs {
		// Only the path matters: sitemap entries carry absolute production
		// URLs, which differ from c.siteURL under test.
		loc, err := url.Parse(strings.TrimSpace(entry.Loc))
		if err != nil {
			continue
		}
		path := normalizePath(loc.Path)
		if !inScope(path) || seen[path] {
			continue
		}
		seen[path] = true
		pages = append(pages, Page{
			Path:    path,
			Title:   titleFromPath(path),
			Section: sectionFromPath(path),
			URL:     c.siteURL + path,
		})
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("docs sitemap contained no pages under %s", PathPrefix)
	}

	// Path order groups each section's pages together, which is also the order
	// the model reads them in.
	sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })

	return pages, nil
}

// Content fetches the markdown counterpart of an indexed page. The caller is
// responsible for validating path against the index first; Content trusts it.
func (c *Client) Content(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.siteURL+path+".md", nil)
	if err != nil {
		return "", fmt.Errorf("build docs page request: %w", err)
	}
	req.Header.Set("Accept", "text/markdown")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch docs page: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch docs page %s: unexpected status %d", path, resp.StatusCode)
	}

	// Read one byte past the cap so an oversized page is detected rather than
	// silently truncated. Half a page is worse than no page here: the model
	// has no way to tell it was cut off, so it would present incomplete
	// product guidance as though it were complete.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return "", fmt.Errorf("read docs page %s: %w", path, err)
	}
	if len(body) > maxPageBytes {
		return "", fmt.Errorf("read docs page %s: page exceeds the %d byte limit", path, maxPageBytes)
	}

	content := strings.TrimSpace(string(body))

	// The docs site serves its HTML error shell with a text/markdown
	// content-type, so the header cannot be trusted to tell markdown from a
	// rendered error page. Sniff the body instead.
	if looksLikeHTML(content) {
		return "", fmt.Errorf("fetch docs page %s: response was HTML, not markdown", path)
	}

	return content, nil
}

// normalizePath strips the trailing slash the sitemap emits so paths match the
// form platform_get_doc accepts and the ".md" suffix appends cleanly.
func normalizePath(path string) string {
	if path == "/" {
		return path
	}
	return strings.TrimSuffix(path, "/")
}

// inScope reports whether a site path belongs to the indexed corpus. The
// separator check keeps a sibling like /docs/ai-control-plane-overview out.
func inScope(path string) bool {
	return path == PathPrefix || strings.HasPrefix(path, PathPrefix+"/")
}

func sectionFromPath(path string) string {
	rest := strings.TrimPrefix(path, PathPrefix+"/")
	if rest == path || rest == "" {
		return ""
	}
	section, _, _ := strings.Cut(rest, "/")
	return section
}

func titleFromPath(path string) string {
	if path == PathPrefix {
		return "AI control plane"
	}

	slug := path[strings.LastIndex(path, "/")+1:]
	if slug == "" {
		return path
	}

	words := []rune(strings.ReplaceAll(slug, "-", " "))
	words[0] = unicode.ToUpper(words[0])
	return string(words)
}

func looksLikeHTML(content string) bool {
	head := strings.ToLower(content)
	if len(head) > 512 {
		head = head[:512]
	}
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

// Package changelog exposes the public Speakeasy platform changelog as a
// read-only platform tool for the project's managed assistant. Entries come
// from the JSON feed at speakeasy.com/changelog/data/gram.json — the same
// build-time artifact the /changelog page's client island renders — filtered
// to the platform + dashboard products and sorted newest first.
package changelog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// DefaultFeedURL is the production changelog feed consumed by the client.
const DefaultFeedURL = "https://www.speakeasy.com/changelog/data/gram.json"

// releaseURLFormat builds the public permalink for a release, matching the
// permalinks rendered on the changelog page itself.
const releaseURLFormat = "https://www.speakeasy.com/changelog/release/%s?product=mcp-platform"

// FetchTimeout bounds a single feed fetch (dial through response read) so a
// stalled marketing-site response can't hang the tool. Applied both to the
// detached refresh context inside the client and to the dedicated guardian
// client at the call site.
const FetchTimeout = 15 * time.Second

// supportedProducts are the changelog product areas this tool serves. The feed
// is already scoped to these, but the client filters to them defensively so a
// default (unfiltered) request can never surface release notes from another
// area, and the tool validates the caller's `product` against the same set.
var supportedProducts = map[string]bool{"platform": true, "dashboard": true}

// refreshKey is the singleflight key coalescing concurrent feed refreshes.
const refreshKey = "entries"

const (
	// cacheTTL bounds how often the feed is re-fetched. The changelog updates
	// at most a few times per week, so the TTL only bounds staleness within a
	// conversation.
	cacheTTL = 15 * time.Minute

	// maxResponseBytes caps how much of the feed is read. The current feed
	// weighs a few hundred KB; 8MB leaves generous headroom without letting a
	// misbehaving response exhaust memory.
	maxResponseBytes = 8 << 20

	// maxDetailsLen caps the itemized notes carried per entry so a single
	// tool result cannot flood the model context.
	maxDetailsLen = 6000
)

// Entry is one release note from the changelog feed.
type Entry struct {
	Version string `json:"version"`
	Product string `json:"product,omitempty"`
	Date    string `json:"date,omitempty"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
	URL     string `json:"url,omitempty"`
	Details string `json:"details,omitempty"`
}

// feedPost mirrors one element of the gram.json feed payload.
type feedPost struct {
	Content  string `json:"content"`
	Metadata struct {
		Version string `json:"version"`
		Date    string `json:"date"`
		Product string `json:"product"`
	} `json:"metadata"`
}

// Client fetches and caches changelog entries. Safe for concurrent use: the
// cache is guarded by a mutex held only for in-memory reads/writes, and network
// I/O happens outside it, coalesced by singleflight so a burst of tool calls
// triggers a single fetch rather than hammering the marketing site.
type Client struct {
	httpClient *guardian.HTTPClient
	feedURL    string
	refresh    singleflight.Group

	mu        sync.Mutex
	entries   []Entry
	fetchedAt time.Time
}

// NewClient returns a client that reads the changelog feed at feedURL (use
// DefaultFeedURL in production wiring) through httpClient.
func NewClient(httpClient *guardian.HTTPClient, feedURL string) *Client {
	return &Client{
		httpClient: httpClient,
		feedURL:    feedURL,
		refresh:    singleflight.Group{},
		mu:         sync.Mutex{},
		entries:    nil,
		fetchedAt:  time.Time{},
	}
}

// Entries returns the parsed changelog, refreshing the cache when stale. When
// a refresh fails but a previous fetch succeeded, the stale entries are
// returned instead of the error so transient marketing-site outages degrade
// freshness rather than break the tool.
func (c *Client) Entries(ctx context.Context) ([]Entry, error) {
	c.mu.Lock()
	cached := c.entries
	fresh := cached != nil && time.Since(c.fetchedAt) < cacheTTL
	c.mu.Unlock()

	if fresh {
		return cached, nil
	}

	// Refresh outside the cache mutex so a slow fetch never blocks other
	// callers on c.mu. singleflight coalesces a burst of tool calls into one
	// fetch, which runs on a context detached from any single caller (but
	// bounded by FetchTimeout) so one canceled request can't abort the shared
	// refresh. Each caller still honors its own deadline via the select below,
	// falling back to stale entries rather than waiting on the network.
	ch := c.refresh.DoChan(refreshKey, func() (any, error) {
		// A caller that saw a stale cache may land here just after a
		// concurrent flight refreshed it; recheck before paying for a fetch.
		c.mu.Lock()
		if c.entries != nil && time.Since(c.fetchedAt) < cacheTTL {
			entries := c.entries
			c.mu.Unlock()
			return entries, nil
		}
		c.mu.Unlock()

		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), FetchTimeout)
		defer cancel()

		entries, err := c.fetch(fetchCtx)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		c.entries = entries
		c.fetchedAt = time.Now()
		c.mu.Unlock()

		return entries, nil
	})

	select {
	case <-ctx.Done():
		if cached != nil {
			return cached, nil
		}
		return nil, fmt.Errorf("await changelog refresh: %w", ctx.Err())
	case res := <-ch:
		if res.Err != nil {
			if cached != nil {
				return cached, nil
			}
			return nil, res.Err
		}
		entries, ok := res.Val.([]Entry)
		if !ok {
			return nil, fmt.Errorf("changelog refresh returned unexpected type %T", res.Val)
		}
		return entries, nil
	}
}

func (c *Client) fetch(ctx context.Context) ([]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build changelog feed request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch changelog feed: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch changelog feed: unexpected status %d", resp.StatusCode)
	}

	var posts []feedPost
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&posts); err != nil {
		return nil, fmt.Errorf("decode changelog feed: %w", err)
	}
	if len(posts) == 0 {
		return nil, fmt.Errorf("empty changelog feed")
	}

	// Keep only supported product areas so a default request never surfaces
	// release notes from another area, then sort newest-first (version
	// descending on same-day ties) so `limit` returns the most recent releases
	// regardless of the feed's array order.
	supported := posts[:0]
	for _, post := range posts {
		if supportedProducts[post.Metadata.Product] {
			supported = append(supported, post)
		}
	}
	posts = supported

	sort.SliceStable(posts, func(i, j int) bool {
		di, dj := parseFeedDate(posts[i].Metadata.Date), parseFeedDate(posts[j].Metadata.Date)
		if !di.Equal(dj) {
			return di.After(dj)
		}
		return versionGreater(posts[i].Metadata.Version, posts[j].Metadata.Version)
	})

	entries := make([]Entry, 0, len(posts))
	for _, post := range posts {
		entries = append(entries, toEntry(post))
	}
	return entries, nil
}

// parseFeedDate parses a feed post's date, accepting both the RFC3339 form the
// feed emits and a bare calendar date, returning the zero time when neither
// parses (such entries sort last).
func parseFeedDate(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

// versionGreater reports whether dotted version a is greater than b, compared
// numerically segment by segment (e.g. "0.90.1" > "0.89.0"). A missing or
// non-numeric segment is treated as 0.
func versionGreater(a, b string) bool {
	pa, pb := parseVersion(a), parseVersion(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return len(pa) > len(pb)
}

func parseVersion(v string) []int {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(strings.TrimSpace(p))
	}
	// Trailing zero segments don't change a version's order ("1.0" == "1");
	// strip them so the length tie-breaker can't call such versions unequal.
	for len(out) > 0 && out[len(out)-1] == 0 {
		out = out[:len(out)-1]
	}
	return out
}

func toEntry(post feedPost) Entry {
	title, summary, details := splitContent(post.Content)
	// Rune-safe truncation: a byte slice could split a multi-byte character
	// and hand the model invalid UTF-8 at the cut point.
	if truncated := conv.TruncateString(details, maxDetailsLen); truncated != details {
		details = truncated + "…"
	}

	date := post.Metadata.Date
	if t, err := time.Parse(time.RFC3339, date); err == nil {
		date = t.Format("2006-01-02")
	}

	var version, permalink string
	if v := post.Metadata.Version; v != "" {
		version = "v" + v
		permalink = fmt.Sprintf(releaseURLFormat, v)
	}
	// If the feed's markdown shape drifts and no "### " title parses, fall
	// back to the version so entries stay identifiable; the content itself is
	// preserved in summary/details either way.
	if title == "" {
		title = version
	}

	return Entry{
		Version: version,
		Product: post.Metadata.Product,
		Date:    date,
		Title:   title,
		Summary: summary,
		URL:     permalink,
		Details: details,
	}
}

// splitContent breaks a feed post's markdown body into its leading "### "
// title, the prose summary paragraph that follows it, and the remaining
// itemized notes (the "#### Features" / "#### Bug Fixes" sections).
func splitContent(content string) (title string, summary string, details string) {
	rest := strings.TrimSpace(content)

	if after, ok := strings.CutPrefix(rest, "### "); ok {
		line, remainder, _ := strings.Cut(after, "\n")
		title = strings.TrimSpace(line)
		rest = strings.TrimSpace(remainder)
	}

	if !strings.HasPrefix(rest, "#") {
		paragraph, remainder, _ := strings.Cut(rest, "\n\n")
		summary = strings.Join(strings.Fields(paragraph), " ")
		rest = strings.TrimSpace(remainder)
	}

	return title, summary, rest
}

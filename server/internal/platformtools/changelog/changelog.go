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
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// DefaultFeedURL is the production changelog feed consumed by the client.
const DefaultFeedURL = "https://www.speakeasy.com/changelog/data/gram.json"

// releaseURLFormat builds the public permalink for a release, matching the
// permalinks rendered on the changelog page itself.
const releaseURLFormat = "https://www.speakeasy.com/changelog/release/%s?product=mcp-platform"

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

// Client fetches and caches changelog entries. Safe for concurrent use; a
// single in-flight fetch is serialized behind the mutex so bursts of tool
// calls hit the cache rather than the marketing site.
type Client struct {
	httpClient *guardian.HTTPClient
	feedURL    string

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
	defer c.mu.Unlock()

	if c.entries != nil && time.Since(c.fetchedAt) < cacheTTL {
		return c.entries, nil
	}

	entries, err := c.fetch(ctx)
	if err != nil {
		if c.entries != nil {
			return c.entries, nil
		}
		return nil, err
	}

	c.entries = entries
	c.fetchedAt = time.Now()
	return c.entries, nil
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
		return nil, fmt.Errorf("decode changelog feed: no entries found")
	}

	entries := make([]Entry, 0, len(posts))
	for _, post := range posts {
		entries = append(entries, toEntry(post))
	}
	return entries, nil
}

func toEntry(post feedPost) Entry {
	title, summary, details := splitContent(post.Content)
	if len(details) > maxDetailsLen {
		details = details[:maxDetailsLen] + "…"
	}

	date := post.Metadata.Date
	if t, err := time.Parse(time.RFC3339, date); err == nil {
		date = t.Format("2006-01-02")
	}

	var permalink string
	if post.Metadata.Version != "" {
		permalink = fmt.Sprintf(releaseURLFormat, post.Metadata.Version)
	}

	return Entry{
		Version: "v" + post.Metadata.Version,
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

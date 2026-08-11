// Package repometa looks up what a code host publishes about an MCP server's
// declared source repository.
//
// A repository is the closest thing a package has to a public track record:
// how many people watch it, whether it is archived, when it was last pushed,
// and how many accounts have committed to it. Those are the "is this real and
// maintained" signals an approver reads first, and they come from a public
// API needing no credential.
//
// Two caveats hold everywhere here. The repository is the *publisher's* claim
// — nothing verifies that the named repository actually builds the package an
// installer downloads, so a popular repository does not vouch for the
// artifact. And every number is a popularity proxy, not a safety signal: a
// typosquat can point at the real project's repository, which is exactly why
// the claim caveat is stated on the section wherever it renders.
//
// Only GitHub is consulted: it hosts the overwhelming share of published MCP
// servers, and a repository on a host this package does not recognize is
// reported as unsupported rather than as a gap, so the panel says "not
// checked" instead of "check failed".
package repometa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// maxResponseBytes bounds a repository response. The repo document is a few
// kilobytes, so the cap exists only to keep a misbehaving response from
// buffering without bound.
const maxResponseBytes = 1 << 20

// Doer issues HTTP requests. `*guardian.HTTPClient` satisfies it, which is
// what the composition root supplies so lookups inherit egress protection.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Stats is what the code host publishes about one repository.
type Stats struct {
	// Host is the code host consulted, currently always "github.com".
	Host string

	// Owner and Name identify the repository on the host.
	Owner string
	Name  string

	// Stars and Forks are the host's popularity counters.
	Stars int
	Forks int

	// OpenIssues is the host's open-issue counter. On GitHub this includes
	// open pull requests, so it reads as "open items", not a defect count.
	OpenIssues int

	// Archived reports the owner froze the repository — no further commits or
	// issues. An archived repository behind an actively-installed package is a
	// maintenance signal an approver should see.
	Archived bool

	// CreatedAt is when the repository was created.
	CreatedAt time.Time

	// PushedAt is the last commit push anywhere in the repository, which is
	// the commit-recency signal — unlike updated_at, it does not move on
	// stars or metadata edits.
	PushedAt time.Time

	// ContributorCount is how many accounts have commits in the repository,
	// zero when the host did not answer the extra contributors request. Zero
	// must render as unknown: every repository with commits has at least one.
	ContributorCount int
}

// Client looks repositories up over the public GitHub API.
type Client struct {
	http    Doer
	baseURL string
	token   string
}

// Option overrides a client default.
type Option func(*Client)

// WithBaseURL points lookups at a different host, for a test server. Only
// repositories on github.com are recognized regardless of the endpoint.
func WithBaseURL(base string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(base, "/") }
}

// WithToken authenticates lookups. Unauthenticated GitHub API calls share a
// small per-IP hourly budget, so production supplies a token where one is
// configured; lookups run identically without one until the budget runs out,
// after which they fail and land in the document's gaps.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// NewClient builds a client against the public GitHub API. The Doer should be
// guardian-backed in production so lookups are subject to egress policy.
func NewClient(doer Doer, options ...Option) *Client {
	client := &Client{
		http:    doer,
		baseURL: "https://api.github.com",
		token:   "",
	}
	for _, option := range options {
		option(client)
	}

	return client
}

// Lookup fetches what GitHub publishes about the repository a package
// declared.
//
// A repository the host does not know returns (nil, nil): the publisher
// pointing at a missing repository is an ordinary (and telling) outcome, not
// an error. A reference on an unsupported host also returns (nil, nil) — not
// consulted is not a failure.
func (c *Client) Lookup(ctx context.Context, repositoryURL string) (*Stats, error) {
	owner, name, ok := ParseGitHubRepo(repositoryURL)
	if !ok {
		return nil, nil
	}

	var doc struct {
		FullName   string    `json:"full_name"`
		Stars      int       `json:"stargazers_count"`
		Forks      int       `json:"forks_count"`
		OpenIssues int       `json:"open_issues_count"`
		Archived   bool      `json:"archived"`
		CreatedAt  time.Time `json:"created_at"`
		PushedAt   time.Time `json:"pushed_at"`
	}

	endpoint := c.baseURL + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
	found, _, err := c.get(ctx, endpoint, &doc)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	if doc.FullName == "" {
		return nil, fmt.Errorf("unrecognized repository response for %s/%s", owner, name)
	}

	stats := &Stats{
		Host:             "github.com",
		Owner:            owner,
		Name:             name,
		Stars:            doc.Stars,
		Forks:            doc.Forks,
		OpenIssues:       doc.OpenIssues,
		Archived:         doc.Archived,
		CreatedAt:        doc.CreatedAt,
		PushedAt:         doc.PushedAt,
		ContributorCount: 0,
	}

	// The contributor count rides GitHub's pagination rather than a dedicated
	// field: one item per page makes the last-page number the total. The
	// extra request is best-effort — the count is a nice-to-have on top of a
	// lookup that already succeeded, so a failure leaves it unknown rather
	// than failing the section.
	if count, err := c.contributorCount(ctx, owner, name); err == nil {
		stats.ContributorCount = count
	}

	return stats, nil
}

func (c *Client) contributorCount(ctx context.Context, owner, name string) (int, error) {
	endpoint := c.baseURL + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/contributors?per_page=1&anon=false"

	var page []struct {
		Login string `json:"login"`
	}
	found, header, err := c.get(ctx, endpoint, &page)
	if err != nil {
		return 0, err
	}
	if !found || len(page) == 0 {
		return 0, nil
	}

	if last, ok := lastPageNumber(header.Get("Link")); ok {
		return last, nil
	}

	// No Link header means everything fit on the single one-item page.
	return len(page), nil
}

// lastPagePattern finds the page number of the `rel="last"` entry in a GitHub
// Link header.
var lastPagePattern = regexp.MustCompile(`[?&]page=(\d+)>;\s*rel="last"`)

func lastPageNumber(link string) (int, bool) {
	match := lastPagePattern.FindStringSubmatch(link)
	if match == nil {
		return 0, false
	}

	count, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}

	return count, true
}

// get fetches and decodes an API document, reporting whether the resource
// exists and returning the response header for pagination reads.
func (c *Client) get(ctx context.Context, endpoint string, into any) (bool, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, nil, fmt.Errorf("build repository metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return false, nil, fmt.Errorf("fetch repository metadata: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode == http.StatusNotFound {
		return false, nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, nil, fmt.Errorf("fetch repository metadata: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return false, nil, fmt.Errorf("read repository metadata: %w", err)
	}
	if len(body) > maxResponseBytes {
		return false, nil, fmt.Errorf("repository metadata response exceeded the %d-byte limit", maxResponseBytes)
	}

	if err := json.Unmarshal(body, into); err != nil {
		return false, nil, fmt.Errorf("decode repository metadata: %w", err)
	}

	return true, resp.Header, nil
}

// ParseGitHubRepo extracts owner and repository from the URL shapes package
// registries store: https URLs with or without a `git+` prefix or `.git`
// suffix, `git://` and `ssh://` URLs, scp-style `git@github.com:owner/repo`,
// and the `github:owner/repo` shorthand.
func ParseGitHubRepo(raw string) (string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}

	trimmed = strings.TrimPrefix(trimmed, "git+")

	if shorthand, ok := strings.CutPrefix(trimmed, "github:"); ok {
		return splitOwnerRepo(shorthand)
	}
	if scp, ok := strings.CutPrefix(trimmed, "git@github.com:"); ok {
		return splitOwnerRepo(scp)
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", "", false
	}
	if !strings.EqualFold(u.Hostname(), "github.com") {
		return "", "", false
	}

	return splitOwnerRepo(strings.TrimPrefix(u.Path, "/"))
}

// splitOwnerRepo reads the leading owner/repo pair, tolerating a `.git`
// suffix and trailing path segments such as `/tree/main/packages/server`,
// which monorepo packages publish routinely.
func splitOwnerRepo(path string) (string, string, bool) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 {
		return "", "", false
	}

	owner := segments[0]
	name := strings.TrimSuffix(segments[1], ".git")
	if owner == "" || name == "" {
		return "", "", false
	}

	return owner, name, true
}

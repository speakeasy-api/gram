// Package research provides the MCP research agent's web tools: search and
// page fetch. They exist for the questions no deterministic source answers —
// is this vendor real, do they publish a security policy, has anything been
// written about this server — and they are the only research tools the agent
// gets; everything deterministic reaches it through the stored evidence
// document instead.
//
// The defining constraint, stated here because every tool in this package
// inherits it: everything these tools return is untrusted. Pages are authored
// by the party under review, and search results are cheap to seed. Results
// are data for the caller to weigh and cite, never instructions, and nothing
// fetched may drive further tool calls on its own authority.
package research

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// searchModel is the completion model search rides on. The model's prose is
// discarded — only the plugin's citations are returned — so the cheapest
// allowlisted model does the job.
const searchModel = "google/gemini-3.5-flash-lite"

// defaultSearchResults and maxSearchResults bound one search's result count.
const (
	defaultSearchResults = 5
	maxSearchResults     = 10
)

// FetchTimeout bounds one page fetch. Exported for the wiring layer, which
// applies it to the guardian client this package receives.
const FetchTimeout = 15 * time.Second

// maxFetchBytes caps how much of a page is read. Fetches truncate rather than
// fail at the cap: a partial page is still researchable material, and the
// result says so.
const maxFetchBytes = 2 << 20

// maxContentChars caps the text returned from one fetch after HTML
// extraction, keeping a single page from flooding the agent's context.
const maxContentChars = 40_000

// maxFetchesPerChat bounds how many pages one run may fetch. The agent
// follows links derived from search results about an untrusted target;
// without a budget a seeded result chain could turn one run into an
// unbounded crawl.
const maxFetchesPerChat = 25

// fetchBudgetWindow is how long a run's fetch count is retained. Runs are far
// shorter than this; the window only bounds the tracking map.
const fetchBudgetWindow = time.Hour

// CompletionProvider is the slice of the OpenRouter client search needs.
// *openrouter.ChatClient satisfies it.
type CompletionProvider interface {
	GetCompletion(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error)
}

// SearchResult is one cited web result.
type SearchResult struct {
	// Title is the page title as the search engine reports it.
	Title string `json:"title,omitempty"`

	// URL is the result's address, which is also its citation.
	URL string `json:"url"`

	// Snippet is the search engine's excerpt for the page — third-party
	// text, untrusted like the page itself.
	Snippet string `json:"snippet,omitempty"`
}

// SearchClient runs web searches through OpenRouter's web-search plugin, so
// search shares the org's existing OpenRouter billing instead of introducing
// a search vendor and secret.
type SearchClient struct {
	completions CompletionProvider
}

// NewSearchClient builds a search client over the supplied completion
// provider.
func NewSearchClient(completions CompletionProvider) *SearchClient {
	return &SearchClient{completions: completions}
}

// Search runs one web search and returns the plugin's cited results. The
// model's own prose is discarded: the citations are the deliverable, and
// keeping them free of model narration keeps this tool a search, not a
// summarizer. No results with a nil error is a real answer.
func (c *SearchClient) Search(ctx context.Context, orgID, projectID, query string, maxResults int) ([]SearchResult, error) {
	temperature := 0.0
	response, err := c.completions.GetCompletion(ctx, openrouter.CompletionRequest{
		OrgID:     orgID,
		ProjectID: projectID,
		Messages: []or.ChatMessages{
			or.CreateChatMessagesUser(or.ChatUserMessage{
				Role:    or.ChatUserMessageRoleUser,
				Content: or.CreateChatUserMessageContentStr(query),
				Name:    nil,
			}),
		},
		Tools:       nil,
		Temperature: &temperature,
		Model:       searchModel,
		Stream:      false,
		// The chat key pays by decision, with the mcp-research source
		// carrying the distinct spend attribution within it.
		UsageSource:    billing.ModelUsageSourceMCPResearch,
		ChatID:         uuid.Nil,
		UserID:         "",
		ExternalUserID: "",
		UserEmail:      "",
		HTTPMetadata:   nil,
		APIKeyID:       "",
		KeyType:        openrouter.KeyTypeChat,
		KeySlot:        "",
		JSONSchema:     nil,
		// The answer text is discarded, so reasoning tokens would be pure
		// waste.
		Reasoning:                 &openrouter.Reasoning{Effort: "none", MaxTokens: nil, Exclude: nil, Enabled: nil},
		CacheControl:              nil,
		NormalizeOutboundMessages: false,
		WebSearch:                 &openrouter.WebSearchOptions{MaxResults: maxResults},
	})
	if err != nil {
		return nil, fmt.Errorf("run web search: %w", err)
	}

	results := make([]SearchResult, 0, len(response.Annotations))
	for _, annotation := range response.Annotations {
		citation := annotation.URLCitation
		if annotation.Type != "url_citation" || citation == nil || citation.URL == "" {
			continue
		}
		results = append(results, SearchResult{
			Title:   citation.Title,
			URL:     citation.URL,
			Snippet: citation.Content,
		})
		if len(results) == maxResults {
			break
		}
	}

	return results, nil
}

// fetchBudget counts fetches per run, keyed by the assistant chat id. It is
// per-replica state: the point is damping a runaway crawl, not exact
// cross-replica accounting.
type fetchBudget struct {
	mu sync.Mutex

	// startedAt records when each key's window opened, so stale runs age out
	// of the map.
	startedAt map[string]time.Time
	counts    map[string]int
}

func newFetchBudget() *fetchBudget {
	return &fetchBudget{
		mu:        sync.Mutex{},
		startedAt: make(map[string]time.Time),
		counts:    make(map[string]int),
	}
}

// take consumes one fetch from the key's budget, reporting whether one was
// available. Every call prunes expired windows so the map stays bounded by
// the runs active within the current window. An empty key — a caller outside
// an assistant run — shares one conservative bucket rather than getting an
// unlimited one.
func (b *fetchBudget) take(key string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for other, started := range b.startedAt {
		if now.Sub(started) >= fetchBudgetWindow {
			delete(b.startedAt, other)
			delete(b.counts, other)
		}
	}

	if b.counts[key] >= maxFetchesPerChat {
		return false
	}

	if _, seen := b.startedAt[key]; !seen {
		b.startedAt[key] = now
	}
	b.counts[key]++

	return true
}

// clipRunes cuts s to at most limit runes on a rune boundary, reporting
// whether anything was cut.
func clipRunes(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	runes := 0
	for i := range s {
		if runes == limit {
			return s[:i], true
		}
		runes++
	}

	return s, false
}

// collapseWhitespace folds runs of whitespace into single spaces and blank
// lines into one, keeping extracted page text compact without losing
// paragraph structure.
func collapseWhitespace(s string) string {
	var out strings.Builder
	out.Grow(len(s))

	lines := strings.Split(s, "\n")
	blank := 0
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			blank++
			continue
		}
		if out.Len() > 0 {
			if blank > 0 {
				out.WriteString("\n\n")
			} else {
				out.WriteString("\n")
			}
		}
		blank = 0
		out.WriteString(strings.Join(fields, " "))
	}

	return out.String()
}

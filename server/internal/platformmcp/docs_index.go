package platformmcp

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	// maxDocsExcerpts and maxDocsExcerptBytes bound one search result. They are
	// a content budget, not a paging window: the corpus is small and every
	// excerpt is model-visible, so a wider result would spend context without
	// adding an answer.
	maxDocsExcerpts     = 5
	maxDocsExcerptBytes = 2 * 1024
	// maxDocsQueryBytes bounds the query. Scoring is linear in query terms, and
	// nothing useful is expressed past this length.
	maxDocsQueryBytes = 512
	// relevanceFloorPercent keeps only results scoring within this percentage
	// of the best one. Five weak citations read as five answers; a reader
	// cannot tell which one the claim came from.
	relevanceFloorPercent = 60
)

// DocsExcerpt is one cited passage. Every field except Excerpt is the citation:
// a reader must be able to see what the passage is, where it came from, how old
// it is, and where to verify it — without a second call.
type DocsExcerpt struct {
	URI          string   `json:"uri"`
	Title        string   `json:"title"`
	Heading      string   `json:"heading,omitempty"`
	Excerpt      string   `json:"excerpt"`
	Source       string   `json:"source"`
	Owner        string   `json:"owner,omitempty"`
	ObservedAt   string   `json:"observed_at,omitempty"`
	RevalidateBy string   `json:"revalidate_by,omitempty"`
	Stale        bool     `json:"stale,omitempty"`
	Links        []string `json:"links,omitempty"`
	// DocsURL is the guide's published page, for a reader who wants to open it
	// rather than have it quoted.
	DocsURL string `json:"docs_url,omitempty"`
}

// DocsIndex is the provider-neutral retrieval boundary. The public tool is
// defined by this interface, not by whatever indexes behind it: a vendor search
// backend is one possible implementation and never a runtime dependency or a
// public surface.
//
// Implementations retrieve only from reviewed, allowlisted, pinned content.
// Live web or provider retrieval is not a permitted implementation.
type DocsIndex interface {
	Search(ctx context.Context, query string, limit int) ([]DocsExcerpt, error)
}

// docsChunk is one indexed passage: a heading and the prose under it.
type docsChunk struct {
	resource SetupResource
	heading  string
	body     string
	terms    map[string]int
}

// MemoryDocsIndex searches the pinned corpus in process. It holds no network
// client and no vendor dependency, which is what lets the public tool stay
// provider-neutral while the corpus is small enough that nothing more is
// warranted.
type MemoryDocsIndex struct {
	chunks []docsChunk
	now    func() time.Time
}

var _ DocsIndex = (*MemoryDocsIndex)(nil)

// NewMemoryDocsIndex indexes the reviewed corpus by markdown heading. now is
// injected so freshness is evaluated against a test's clock rather than the
// wall clock at process start.
func NewMemoryDocsIndex(resources []SetupResource, now func() time.Time) *MemoryDocsIndex {
	if now == nil {
		now = time.Now
	}
	var chunks []docsChunk
	for _, resource := range resources {
		if !validSetupResource(resource) {
			continue
		}
		chunks = append(chunks, chunkByHeading(resource)...)
	}
	return &MemoryDocsIndex{chunks: chunks, now: now}
}

// Search returns the best-scoring excerpts. Content past its grace window is
// omitted entirely rather than ranked low: an unreviewed step that loses on
// score today wins on score tomorrow.
func (i *MemoryDocsIndex) Search(_ context.Context, query string, limit int) ([]DocsExcerpt, error) {
	if i == nil {
		return nil, nil
	}
	if limit <= 0 || limit > maxDocsExcerpts {
		limit = maxDocsExcerpts
	}
	terms := tokenize(truncateRunes(query, maxDocsQueryBytes))
	if len(terms) == 0 {
		return nil, nil
	}

	now := i.now()
	type scored struct {
		chunk docsChunk
		score int
	}
	var hits []scored
	for _, chunk := range i.chunks {
		if chunk.resource.staleness(now) == setupWithheld {
			continue
		}
		if score := scoreChunk(chunk, terms); score > 0 {
			hits = append(hits, scored{chunk: chunk, score: score})
		}
	}
	// Sort by score, then by URI and heading so equal scores order the same way
	// on every call — an unstable ranking would make the same question return
	// different citations.
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		if hits[a].chunk.resource.URI != hits[b].chunk.resource.URI {
			return hits[a].chunk.resource.URI < hits[b].chunk.resource.URI
		}
		return hits[a].chunk.heading < hits[b].chunk.heading
	})

	if len(hits) == 0 {
		return nil, nil
	}
	// Every guide describes OAuth clients and credentials, so a question about
	// one provider scores non-trivially against all of them. Keeping only
	// results close to the best one means a provider-specific question cites
	// that provider rather than padding the answer with four others.
	best := hits[0].score
	cutoff := best * relevanceFloorPercent / 100

	excerpts := make([]DocsExcerpt, 0, limit)
	// One excerpt per resource: five passages from one guide answer less than
	// five guides, and the reader can always read the whole guide by URI.
	seen := make(map[string]bool, limit)
	for _, hit := range hits {
		if len(excerpts) == limit || hit.score < cutoff {
			break
		}
		if seen[hit.chunk.resource.URI] {
			continue
		}
		seen[hit.chunk.resource.URI] = true
		excerpts = append(excerpts, excerptFor(hit.chunk, now))
	}
	return excerpts, nil
}

func excerptFor(chunk docsChunk, now time.Time) DocsExcerpt {
	resource := chunk.resource
	return DocsExcerpt{
		URI:          resource.URI,
		Title:        resource.Title,
		Heading:      chunk.heading,
		Excerpt:      truncateRunes(chunk.body, maxDocsExcerptBytes),
		Source:       resource.Source,
		Owner:        resource.Owner,
		ObservedAt:   formatDocsDate(resource.ObservedAt),
		RevalidateBy: formatDocsDate(resource.RevalidateBy),
		Stale:        resource.staleness(now) == setupStale,
		Links:        resource.Links,
		DocsURL:      resource.DocsURL,
	}
}

func formatDocsDate(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format(time.DateOnly)
}

// chunkByHeading splits guide markdown at its headings. A heading is the unit a
// setup guide is written in — "Generate OAuth credentials", "Find your instance
// URL" — so it is also the unit worth citing.
func chunkByHeading(resource SetupResource) []docsChunk {
	var chunks []docsChunk
	heading := ""
	var body strings.Builder
	flush := func() {
		text := strings.TrimSpace(body.String())
		body.Reset()
		if text == "" {
			return
		}
		chunks = append(chunks, docsChunk{
			resource: resource,
			heading:  heading,
			body:     text,
			terms:    countTerms(tokenize(resource.Title + " " + strings.Join(resource.Aliases, " ") + " " + heading + " " + text)),
		})
	}
	for line := range strings.SplitSeq(resource.indexText(), "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			heading = cleanHeading(line)
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	flush()
	return chunks
}

// cleanHeading turns a markdown heading line into the text a reader should see.
// Guides carry explicit anchors — "## Connect your credentials {#connect}" —
// which are link plumbing for the rendered docs site and noise in a citation.
func cleanHeading(line string) string {
	heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
	if anchor := strings.LastIndex(heading, "{#"); anchor >= 0 && strings.HasSuffix(heading, "}") {
		heading = strings.TrimSpace(heading[:anchor])
	}
	return heading
}

// scoreChunk counts matched query terms, weighting a chunk's identity over its
// prose: a guide whose title or alias is the query is what the caller asked
// for, even when another guide happens to mention the word more often.
func scoreChunk(chunk docsChunk, terms []string) int {
	score := 0
	identity := countTerms(tokenize(chunk.resource.Title + " " + chunk.resource.Provider + " " + strings.Join(chunk.resource.Aliases, " ")))
	headingTerms := countTerms(tokenize(chunk.heading))
	for _, term := range terms {
		if identity[term] > 0 {
			score += 8
		}
		if headingTerms[term] > 0 {
			score += 4
		}
		if count := chunk.terms[term]; count > 0 {
			score += min(count, 3)
		}
	}
	return score
}

func tokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		// One-character tokens carry no signal and match nearly everything.
		if len(field) < 2 {
			continue
		}
		terms = append(terms, field)
	}
	return terms
}

func countTerms(terms []string) map[string]int {
	counts := make(map[string]int, len(terms))
	for _, term := range terms {
		counts[term]++
	}
	return counts
}

// truncateRunes cuts at a rune boundary so a truncated excerpt is still valid
// UTF-8, and marks the cut so the reader knows to open the resource.
func truncateRunes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	const ellipsis = "\n…"
	cut := limit - len(ellipsis)
	for cut > 0 && !isRuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + ellipsis
}

func isRuneStart(b byte) bool {
	return b&0xC0 != 0x80
}

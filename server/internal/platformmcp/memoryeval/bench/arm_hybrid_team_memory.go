package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// DefaultEmbeddingModel and defaultEmbeddingDimensions match the business
	// memory store, so a win here can reuse that setup rather than a new one.
	DefaultEmbeddingModel      = "perplexity/pplx-embed-v1-0.6b"
	defaultEmbeddingDimensions = 1024

	// embedBatchSize bounds the inputs sent in one embedding call.
	embedBatchSize = 64

	// rrfK is the reciprocal rank fusion constant. 60 is the value from the
	// original paper and the usual default; it damps the weight of the very
	// top ranks so neither list dominates.
	rrfK = 60

	// hybridCandidates is how many hits each of the full-text and vector
	// lists contributes before fusion.
	hybridCandidates = 20

	// hybridSnippetRunes caps the text of a hit only the vector list found,
	// matching search_team_memory's snippet size.
	hybridSnippetRunes = 1200
)

// Embedder turns texts into vectors, one per input, in order.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// OpenRouterEmbedder embeds through the OpenRouter completion client.
type OpenRouterEmbedder struct {
	Client         openrouter.CompletionClient
	OrganizationID string
	Model          string
	Dimensions     int
}

// NewOpenRouterEmbedder embeds with the business memory model unless model
// is set.
func NewOpenRouterEmbedder(client openrouter.CompletionClient, organizationID, model string) OpenRouterEmbedder {
	if model == "" {
		model = DefaultEmbeddingModel
	}
	return OpenRouterEmbedder{Client: client, OrganizationID: organizationID, Model: model, Dimensions: defaultEmbeddingDimensions}
}

// Embed implements Embedder in batches.
func (e OpenRouterEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(inputs))
	for batch := range slices.Chunk(inputs, embedBatchSize) {
		out, err := e.Client.CreateEmbeddings(ctx, e.OrganizationID, e.Model, batch, openrouter.WithEmbeddingDimensions(e.Dimensions))
		if err != nil {
			return nil, fmt.Errorf("embed with %s: %w", e.Model, err)
		}
		if len(out) != len(batch) {
			return nil, fmt.Errorf("embed with %s: got %d vectors for %d inputs", e.Model, len(out), len(batch))
		}
		vectors = append(vectors, out...)
	}
	return vectors, nil
}

// memoryItem is one summary, memo or digest the hybrid arm can return, with
// its masked text embedded.
type memoryItem struct {
	hit       platformmcp.TeamMemoryHit
	periodEnd time.Time
	vector    []float32
}

// inWindow mirrors search_team_memory's window: summaries and memos by their
// time, digests when their period overlaps the window.
func (m memoryItem) inWindow(from, to time.Time) bool {
	if m.hit.Source.Kind == platformmcp.TeamMemorySourceDigest {
		return m.periodEnd.After(from) && m.hit.At.Before(to)
	}
	return !m.hit.At.Before(from) && m.hit.At.Before(to)
}

// HybridTeamMemoryArm is arm E: search_team_memory with the same tool and
// arguments, but each query also runs a vector search over embeddings of the
// same masked items, and the two ranked lists are merged with reciprocal rank
// fusion. Items are embedded in memory when the arm is built, so the arm
// needs no schema change; it measures retrieval quality, not storage. Only
// masked text is embedded, so a vector can never encode a secret the summary
// job removed. Vector-only hits skip the read audit the tool writes; this is
// an eval arm, not a shipped path.
func HybridTeamMemoryArm(ctx context.Context, logger *slog.Logger, embedder Embedder, target Target) (Arm, error) {
	if embedder == nil {
		return Arm{}, errors.New("hybrid arm needs an embedder")
	}
	searcher, err := newTeamMemorySearcher(logger, target)
	if err != nil {
		return Arm{}, err
	}
	items, err := loadMemoryItems(ctx, embedder, target, searcher.main)
	if err != nil {
		return Arm{}, err
	}

	hybrid := func(ctx context.Context, input platformmcp.SearchTeamMemoryInput) (platformmcp.SearchTeamMemoryOutput, error) {
		limit := input.Limit
		if limit == 0 {
			limit = 10
		}
		// The full-text list goes through the real tool, which validates the
		// input, authorizes the read and resolves the window.
		ftsInput := input
		ftsInput.Limit = hybridCandidates
		fts, err := searcher.service.Search(searcher.withPrincipal(ctx), searcher.principal, ftsInput)
		if err != nil {
			return platformmcp.SearchTeamMemoryOutput{}, fmt.Errorf("search_team_memory: %w", err)
		}
		queryVectors, err := embedder.Embed(ctx, []string{input.Query})
		if err != nil {
			return platformmcp.SearchTeamMemoryOutput{}, fmt.Errorf("embed query: %w", err)
		}
		vector := vectorCandidates(items, queryVectors[0], input.Kinds, fts.From, fts.To, hybridCandidates)
		return platformmcp.SearchTeamMemoryOutput{Hits: fuseRanked(fts.Hits, vector, limit), From: fts.From, To: fts.To}, nil
	}

	search := func(ctx context.Context, query string) (ArmResult, error) {
		out, err := hybrid(ctx, searcher.probeInput(query))
		if err != nil {
			return ArmResult{}, fmt.Errorf("hybrid search %q: %w", query, err)
		}
		return teamMemoryResult(out)
	}
	sweep := func(ctx context.Context) (ArmResult, error) {
		return sweepWith(ctx, target.Corpus, search)
	}
	call := func(ctx context.Context, tool, arguments string) (string, error) {
		if tool != "search_team_memory" {
			return "", fmt.Errorf("unknown tool %q", tool)
		}
		var input platformmcp.SearchTeamMemoryInput
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", fmt.Errorf("decode search_team_memory arguments: %w", err)
		}
		out, err := hybrid(ctx, input)
		if err != nil {
			return "", err
		}
		return encodeOutput(tool, out)
	}
	return Arm{
		Name:   "E: hybrid search_team_memory",
		Search: search,
		Sweep:  sweep,
		Tools: []Tool{
			{Name: "search_team_memory", Description: searchTeamMemoryDescription + " Matches meaning as well as words, so plain-language questions work.", Schema: searchTeamMemorySchema},
		},
		Call:         call,
		Instructions: searcher.instructions(),
	}, nil
}

// loadMemoryItems reads every item search_team_memory could return for the
// main project, masks it the way the tool does on the way out, and embeds
// the masked text.
func loadMemoryItems(ctx context.Context, embedder Embedder, target Target, main Project) ([]memoryItem, error) {
	rows, err := repo.New(target.DB).ListTeamMemoryItems(ctx, repo.ListTeamMemoryItemsParams{OrganizationID: target.OrganizationID, ProjectID: main.ID})
	if err != nil {
		return nil, fmt.Errorf("list team memory items: %w", err)
	}
	redactor := platformmcp.NewChatSearchRedactor()
	items := make([]memoryItem, 0, len(rows))
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		text, _, err := redactor.Text(ctx, row.Body)
		if err != nil {
			return nil, fmt.Errorf("redact team memory item: %w", err)
		}
		snippet, truncated := text, false
		if runes := []rune(text); len(runes) > hybridSnippetRunes {
			snippet, truncated = string(runes[:hybridSnippetRunes]), true
		}
		source := platformmcp.TeamMemorySource{Kind: row.Kind, ID: row.ID.String(), ChatID: ""}
		if row.ChatID.Valid {
			source.ChatID = row.ChatID.UUID.String()
		}
		item := memoryItem{
			hit:       platformmcp.TeamMemoryHit{Source: source, Text: snippet, Truncated: truncated, At: row.At.Time.UTC(), AuthorUserID: row.AuthorUserID, Level: row.Level, PeriodStart: ""},
			periodEnd: time.Time{},
			vector:    nil,
		}
		if row.PeriodStart.Valid {
			item.hit.PeriodStart = row.PeriodStart.Time.Format(time.DateOnly)
		}
		if row.PeriodEnd.Valid {
			item.periodEnd = row.PeriodEnd.Time.UTC()
		}
		items = append(items, item)
		texts = append(texts, text)
	}
	if len(texts) == 0 {
		return nil, errors.New("no team memory items to embed; write summaries first")
	}
	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed team memory items: %w", err)
	}
	for i := range items {
		items[i].vector = vectors[i]
	}
	return items, nil
}

// vectorCandidates ranks the items that pass the kind and window filters by
// cosine similarity to the query and returns the best n hits.
func vectorCandidates(items []memoryItem, query []float32, kinds []string, from, to time.Time, n int) []platformmcp.TeamMemoryHit {
	type scored struct {
		hit   platformmcp.TeamMemoryHit
		score float64
	}
	candidates := make([]scored, 0, len(items))
	for _, item := range items {
		if len(kinds) > 0 && !slices.Contains(kinds, item.hit.Source.Kind) {
			continue
		}
		if !item.inWindow(from, to) {
			continue
		}
		candidates = append(candidates, scored{hit: item.hit, score: cosine(query, item.vector)})
	}
	slices.SortStableFunc(candidates, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		default:
			return 0
		}
	})
	hits := make([]platformmcp.TeamMemoryHit, 0, min(n, len(candidates)))
	for _, c := range candidates[:min(n, len(candidates))] {
		hits = append(hits, c.hit)
	}
	return hits
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// fuseRanked merges ranked lists with reciprocal rank fusion: each hit scores
// the sum of 1/(rrfK+rank) over the lists it appears in, so a hit both lists
// agree on rises above one only either list found. When both lists hold a
// hit, the first list's copy is kept, so full-text hits keep their snippet
// cut around the matching words. Ties keep first-seen order.
func fuseRanked(fts, vector []platformmcp.TeamMemoryHit, limit int) []platformmcp.TeamMemoryHit {
	type fused struct {
		hit   platformmcp.TeamMemoryHit
		score float64
		order int
	}
	byKey := map[string]*fused{}
	order := 0
	for _, list := range [][]platformmcp.TeamMemoryHit{fts, vector} {
		for rank, hit := range list {
			key := hit.Source.Kind + ":" + hit.Source.ID
			entry, ok := byKey[key]
			if !ok {
				entry = &fused{hit: hit, score: 0, order: order}
				order++
				byKey[key] = entry
			}
			entry.score += 1 / float64(rrfK+rank+1)
		}
	}
	all := make([]*fused, 0, len(byKey))
	for _, entry := range byKey {
		all = append(all, entry)
	}
	slices.SortFunc(all, func(a, b *fused) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		default:
			return a.order - b.order
		}
	})
	hits := make([]platformmcp.TeamMemoryHit, 0, min(limit, len(all)))
	for _, entry := range all[:min(limit, len(all))] {
		hits = append(hits, entry.hit)
	}
	return hits
}

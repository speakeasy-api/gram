package businessmemory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector_go "github.com/pgvector/pgvector-go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/businessmemory/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	JudgeName = "business_memory"

	extractionModel      = "google/gemini-3.1-flash-lite"
	extractionPrompt     = "v2"
	extractionTimeout    = 60 * time.Second
	maxExtractedMemories = 12
	maxMemoryBodyBytes   = 8192
	maxContentLabels     = 20
	duplicateSimilarity  = 0.92

	extractionSystemPrompt = `Selectively extract durable business knowledge from the completed session.

Do not turn every exchange into a memory. Prefer a small number of high-value memories, and return an empty memories array when nothing meets the threshold. Extract an item only when at least one of these is true:
- It is likely to help another employee with a future task, decision, or understanding without seeing this transcript.
- It was costly to produce and would require substantial investigation, multi-step tool use, synthesis, computation, or experimentation to reproduce.

Exclude routine or obvious facts, easily rediscovered information, confirmations, restatements, raw tool output without a durable conclusion, one-off execution details with no future value, greetings, plans, speculative ideas, transient progress updates, secrets, credentials, and facts that are only about the assistant's behavior. When uncertain, omit the item. Costly-to-produce information must still satisfy all safety and quality exclusions. Combine overlapping knowledge instead of emitting fragmented memories.

Classify each item as:
- glossary: an internal term and its meaning
- procedure: reusable steps, lookup methods, or operational context
- result: a computed or observed fact tied to a specific absolute period

Resolve relative dates such as "last month" to absolute dates using the cited source turn's created_at timestamp. Do not invent missing dates. Keep each memory self-contained. Cite the one-based transcript turn that best supports it. Content-scope labels use lowercase canonical forms such as "customer:example-corp", "product:gateway", or "topic:tool-usage".`
)

var contentLabelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:_-]{0,127}$`)

type extractionCandidate struct {
	Body         string   `json:"body"`
	MemoryType   string   `json:"memory_type"`
	ContentScope []string `json:"content_scope"`
	SourceTurn   int      `json:"source_turn"`
}

type extractionVerdict struct {
	Memories []extractionCandidate `json:"memories"`
}

type extractionSummary struct {
	Extracted    int            `json:"extracted"`
	Deduplicated int            `json:"deduplicated"`
	ByType       map[string]int `json:"by_type"`
}

type extractionPromptPayload struct {
	Transcript efficacy.Transcript `json:"transcript"`
}

// Judge extracts and stores first-pass organization memories from a completed
// session. Inserts are keyed by evaluation and candidate index so Temporal and
// score-sink retries are idempotent.
type Judge struct {
	logger  *slog.Logger
	tracer  trace.Tracer
	db      *pgxpool.Pool
	client  openrouter.CompletionClient
	limiter *ratelimit.Limiter
}

var (
	_ analysis.Judge          = (*Judge)(nil)
	_ analysis.ScorelessJudge = (*Judge)(nil)
)

func NewJudge(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	client openrouter.CompletionClient,
	limiter *ratelimit.Limiter,
) *Judge {
	return &Judge{
		logger:  logger.With(attr.SlogComponent("business-memory-judge")),
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/businessmemory"),
		db:      db,
		client:  client,
		limiter: limiter,
	}
}

func (j *Judge) Name() string {
	return JudgeName
}

func (j *Judge) SkipScoreSink() bool {
	return true
}

func (j *Judge) Judge(ctx context.Context, in analysis.JudgeInput) (analysis.JudgeResult, error) {
	ctx, span := j.tracer.Start(ctx, "business_memory.extract", trace.WithAttributes(
		attr.OrganizationID(in.OrgID),
		attr.ProjectID(in.ProjectID),
	))
	defer span.End()

	projectID, err := uuid.Parse(in.ProjectID)
	if err != nil {
		err = fmt.Errorf("parse business memory project id: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return analysis.JudgeResult{}, err
	}

	prompt, err := json.Marshal(extractionPromptPayload{Transcript: in.Transcript})
	if err != nil {
		err = fmt.Errorf("marshal business memory extraction payload: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return analysis.JudgeResult{}, err
	}

	raw, model, err := analysis.CallStructured(ctx, j.logger, j.client, j.limiter, in, analysis.StructuredCall{
		Model:        extractionModel,
		SystemPrompt: extractionSystemPrompt,
		Prompt:       string(prompt),
		SchemaName:   "business_memory_extraction",
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"memories"},
			"properties": map[string]any{
				"memories": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"body", "memory_type", "content_scope", "source_turn"},
						"properties": map[string]any{
							"body":          map[string]any{"type": "string"},
							"memory_type":   map[string]any{"type": "string", "enum": []string{"glossary", "procedure", "result"}},
							"content_scope": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"source_turn":   map[string]any{"type": "integer"},
						},
					},
				},
			},
		},
		Timeout: extractionTimeout,
	})
	if err != nil {
		err = fmt.Errorf("call business memory extractor: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return analysis.JudgeResult{}, err
	}

	transcriptTurns := make(map[int]struct{}, len(in.Transcript.Messages))
	for _, message := range in.Transcript.Messages {
		transcriptTurns[message.Index] = struct{}{}
	}
	candidates, err := normalizeExtraction(raw, transcriptTurns)
	if err != nil {
		err = fmt.Errorf("parse business memory extraction: %w: %w", analysis.ErrModelFailure, err)
		span.SetStatus(codes.Error, err.Error())
		return analysis.JudgeResult{}, err
	}

	persisted := candidates
	if len(candidates) > 0 {
		persisted, err = j.persist(ctx, in, projectID, model, candidates)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return analysis.JudgeResult{}, fmt.Errorf("persist business memories: %w: %w", analysis.ErrRetryable, err)
		}
	}

	summary := extractionSummary{
		Extracted:    len(persisted),
		Deduplicated: len(candidates) - len(persisted),
		ByType:       map[string]int{},
	}
	for _, candidate := range persisted {
		summary.ByType[candidate.MemoryType]++
	}
	detail, err := json.Marshal(summary)
	if err != nil {
		err = fmt.Errorf("marshal business memory extraction summary: %w", err)
		span.SetStatus(codes.Error, err.Error())
		return analysis.JudgeResult{}, err
	}

	span.SetStatus(codes.Ok, "")
	return analysis.JudgeResult{
		Verdict: analysis.Verdict{
			Score:  float64(len(persisted)),
			Detail: detail,
		},
		Model:         model,
		PromptVersion: extractionPrompt,
	}, nil
}

func normalizeExtraction(raw string, transcriptTurns map[int]struct{}) ([]extractionCandidate, error) {
	var verdict extractionVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return nil, fmt.Errorf("decode extraction: %w", err)
	}
	if len(verdict.Memories) > maxExtractedMemories {
		return nil, fmt.Errorf("extraction returned %d memories, maximum is %d", len(verdict.Memories), maxExtractedMemories)
	}

	normalized := make([]extractionCandidate, 0, len(verdict.Memories))
	for _, candidate := range verdict.Memories {
		candidate.Body = strings.TrimSpace(candidate.Body)
		if candidate.Body == "" || len(candidate.Body) > maxMemoryBodyBytes || !utf8.ValidString(candidate.Body) {
			return nil, fmt.Errorf("invalid memory body")
		}
		switch candidate.MemoryType {
		case "glossary", "procedure", "result":
		default:
			return nil, fmt.Errorf("invalid memory type %q", candidate.MemoryType)
		}
		if _, ok := transcriptTurns[candidate.SourceTurn]; !ok {
			return nil, fmt.Errorf("source turn %d outside transcript", candidate.SourceTurn)
		}
		if len(candidate.ContentScope) > maxContentLabels {
			return nil, fmt.Errorf("memory has too many content-scope labels")
		}
		labels := make([]string, 0, len(candidate.ContentScope))
		seen := make(map[string]struct{}, len(candidate.ContentScope))
		for _, label := range candidate.ContentScope {
			label = strings.ToLower(strings.TrimSpace(label))
			if !contentLabelPattern.MatchString(label) {
				return nil, fmt.Errorf("invalid content-scope label %q", label)
			}
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			labels = append(labels, label)
		}
		candidate.ContentScope = labels
		normalized = append(normalized, candidate)
	}

	return normalized, nil
}

func isDuplicateMemory(similarity float64) bool {
	return similarity >= duplicateSimilarity
}

func (j *Judge) persist(
	ctx context.Context,
	in analysis.JudgeInput,
	projectID uuid.UUID,
	model string,
	candidates []extractionCandidate,
) ([]extractionCandidate, error) {
	inputs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		inputs = append(inputs, candidate.Body)
	}
	vectors, err := j.client.CreateEmbeddings(
		ctx,
		in.OrgID,
		embeddingModel,
		inputs,
		openrouter.WithEmbeddingDimensions(embeddingDimensions),
	)
	if err != nil {
		return nil, fmt.Errorf("create business memory embeddings: %w", err)
	}
	if len(vectors) != len(candidates) {
		return nil, fmt.Errorf("embedding response had %d vectors, expected %d", len(vectors), len(candidates))
	}

	dbtx, err := j.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin business memory insert: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	if err := enableFilteredVectorScan(ctx, dbtx); err != nil {
		return nil, err
	}

	queries := repo.New(dbtx)
	persisted := make([]extractionCandidate, 0, len(candidates))
	for index, candidate := range candidates {
		embedding := pgvector_go.NewHalfVector(vectors[index])
		nearest, err := queries.GetNearestActiveBusinessMemory(ctx, repo.GetNearestActiveBusinessMemoryParams{
			QueryEmbedding:       embedding,
			ProjectID:            projectID,
			OrganizationID:       in.OrgID,
			SourceEvaluationID:   uuid.NullUUID{UUID: in.EvaluationID, Valid: true},
			SourceCandidateIndex: int32(index),
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("find nearest memory for candidate %d: %w", index, err)
		}
		if err == nil && isDuplicateMemory(nearest.Similarity) {
			continue
		}

		contentScope, err := json.Marshal(candidate.ContentScope)
		if err != nil {
			return nil, fmt.Errorf("marshal business memory content scope: %w", err)
		}
		if err := queries.InsertBusinessMemory(ctx, repo.InsertBusinessMemoryParams{
			ProjectID:            projectID,
			OrganizationID:       in.OrgID,
			Body:                 candidate.Body,
			MemoryType:           candidate.MemoryType,
			StructuralScope:      fmt.Sprintf("company.%s.project.%s", in.OrgID, projectID),
			ContentScope:         contentScope,
			Embedding:            embedding,
			EmbeddingModel:       embeddingModel,
			ExtractionModel:      model,
			SourceEvaluationID:   uuid.NullUUID{UUID: in.EvaluationID, Valid: true},
			SourceCandidateIndex: int32(index),
			SourceChatID:         uuid.NullUUID{UUID: in.ChatID, Valid: true},
			SourceTurn:           pgtype.Int4{Int32: conv.SafeInt32(candidate.SourceTurn), Valid: true},
			SourceAuthorID:       pgtype.Text{String: in.AuthorID, Valid: in.AuthorID != ""},
		}); err != nil {
			return nil, fmt.Errorf("insert business memory candidate %d: %w", index, err)
		}
		persisted = append(persisted, candidate)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit business memory insert: %w", err)
	}
	return persisted, nil
}

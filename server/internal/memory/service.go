package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector_go "github.com/pgvector/pgvector-go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/memory/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// DefaultEmbeddingModel emits 4096-d vectors natively; we truncate to
	// embeddingDimensions via Matryoshka so pgvector's halfvec HNSW dim cap holds.
	DefaultEmbeddingModel     = "qwen/qwen3-embedding-8b"
	DefaultContradictionModel = "mistralai/mistral-medium-3-5"

	// embeddingDimensions pairs with `assistant_memories.embedding halfvec(4000)`.
	// Changing one without the other will fail inserts on dim mismatch.
	embeddingDimensions = 4000

	DefaultHalfLife           = 7 * 24 * time.Hour
	DefaultHardCap            = 5000
	DefaultDedupeUpper        = 0.92
	DefaultDedupeLower        = 0.65
	DefaultForgetAmbiguityGap = 0.05
	DefaultPerResultBytes     = 1024
	DefaultAggregateBytes     = 8192
	DefaultTruncationSuffix   = "…"

	maxContentBytes        = 8192
	contentPreviewMaxRunes = 200
	defaultRecallLimit     = 8
	maxRecallLimit         = 32
	forgetCandidateLimit   = 3
)

type MemoryServiceConfig struct {
	EmbeddingModel     string
	ContradictionModel string
	HalfLife           time.Duration
	HardCap            int
	DedupeUpper        float64
	DedupeLower        float64
	ForgetAmbiguityGap float64
	PerResultBytes     int
	AggregateBytes     int
	TruncationSuffix   string
}

func (cfg MemoryServiceConfig) applyDefaults() MemoryServiceConfig {
	out := cfg
	if out.EmbeddingModel == "" {
		out.EmbeddingModel = DefaultEmbeddingModel
	}
	if out.ContradictionModel == "" {
		out.ContradictionModel = DefaultContradictionModel
	}
	if out.HalfLife <= 0 {
		out.HalfLife = DefaultHalfLife
	}
	if out.HardCap <= 0 {
		out.HardCap = DefaultHardCap
	}
	if out.DedupeUpper == 0 {
		out.DedupeUpper = DefaultDedupeUpper
	}
	if out.DedupeLower == 0 {
		out.DedupeLower = DefaultDedupeLower
	}
	if out.ForgetAmbiguityGap == 0 {
		out.ForgetAmbiguityGap = DefaultForgetAmbiguityGap
	}
	if out.PerResultBytes <= 0 {
		out.PerResultBytes = DefaultPerResultBytes
	}
	if out.AggregateBytes <= 0 {
		out.AggregateBytes = DefaultAggregateBytes
	}
	if out.TruncationSuffix == "" {
		out.TruncationSuffix = DefaultTruncationSuffix
	}
	return out
}

const (
	meterMemoryEmbedDuration      = "gram.memory.embed.duration"
	meterMemoryRecallDuration     = "gram.memory.recall.duration"
	meterMemoryContradictionFails = "gram.memory.contradiction.parse_fail"
	meterMemoryForgetInvocations  = "gram.memory.forget.invocations"
	meterMemorySupersedeDepth     = "gram.memory.supersede.depth"
	meterMemoryRememberOutcome    = "gram.memory.remember.outcome"
)

type memoryMetrics struct {
	embedDuration      metric.Float64Histogram
	recallDuration     metric.Float64Histogram
	contradictionFails metric.Int64Counter
	forgetInvocations  metric.Int64Counter
	supersedeDepth     metric.Int64Histogram
	rememberOutcome    metric.Int64Counter
}

func newMemoryMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *memoryMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/memory")

	embedDuration, err := meter.Float64Histogram(
		meterMemoryEmbedDuration,
		metric.WithDescription("Duration of memory embedding calls in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterMemoryEmbedDuration), attr.SlogError(err))
	}

	recallDuration, err := meter.Float64Histogram(
		meterMemoryRecallDuration,
		metric.WithDescription("End-to-end duration of MemoryService.Recall in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterMemoryRecallDuration), attr.SlogError(err))
	}

	contradictionFails, err := meter.Int64Counter(
		meterMemoryContradictionFails,
		metric.WithDescription("Contradiction-detector calls that returned an error or failed to parse"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterMemoryContradictionFails), attr.SlogError(err))
	}

	forgetInvocations, err := meter.Int64Counter(
		meterMemoryForgetInvocations,
		metric.WithDescription("MemoryService.Forget invocations partitioned by outcome"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterMemoryForgetInvocations), attr.SlogError(err))
	}

	supersedeDepth, err := meter.Int64Histogram(
		meterMemorySupersedeDepth,
		metric.WithDescription("Depth of supersede chains created by Remember"),
		metric.WithUnit("{level}"),
		metric.WithExplicitBucketBoundaries(0, 1, 2, 3, 5, 10),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterMemorySupersedeDepth), attr.SlogError(err))
	}

	rememberOutcome, err := meter.Int64Counter(
		meterMemoryRememberOutcome,
		metric.WithDescription("MemoryService.Remember outcomes partitioned by branch"),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterMemoryRememberOutcome), attr.SlogError(err))
	}

	return &memoryMetrics{
		embedDuration:      embedDuration,
		recallDuration:     recallDuration,
		contradictionFails: contradictionFails,
		forgetInvocations:  forgetInvocations,
		supersedeDepth:     supersedeDepth,
		rememberOutcome:    rememberOutcome,
	}
}

type MemoryService struct {
	logger      *slog.Logger
	tracer      trace.Tracer
	db          *pgxpool.Pool
	completions openrouter.CompletionClient
	audit       *audit.Logger
	metrics     *memoryMetrics

	embeddingModel     string
	contradictionModel string
	halfLife           time.Duration
	hardCap            int
	dedupeUpper        float64
	dedupeLower        float64
	forgetAmbiguityGap float64
	perResultBytes     int
	aggregateBytes     int
	truncationSuffix   string
}

func NewMemoryService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	completions openrouter.CompletionClient,
	auditLogger *audit.Logger,
	cfg MemoryServiceConfig,
) *MemoryService {
	cfg = cfg.applyDefaults()
	componentLogger := logger.With(attr.SlogComponent("memory_service"))
	return &MemoryService{
		logger:             componentLogger,
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/memory"),
		db:                 db,
		completions:        completions,
		audit:              auditLogger,
		metrics:            newMemoryMetrics(meterProvider, componentLogger),
		embeddingModel:     cfg.EmbeddingModel,
		contradictionModel: cfg.ContradictionModel,
		halfLife:           cfg.HalfLife,
		hardCap:            cfg.HardCap,
		dedupeUpper:        cfg.DedupeUpper,
		dedupeLower:        cfg.DedupeLower,
		forgetAmbiguityGap: cfg.ForgetAmbiguityGap,
		perResultBytes:     cfg.PerResultBytes,
		aggregateBytes:     cfg.AggregateBytes,
		truncationSuffix:   cfg.TruncationSuffix,
	}
}

// Validate checks that the service's configuration is internally consistent.
// Call sites should invoke this once at startup; the constructor stays
// infallible so wiring in start.go reads cleanly.
func (s *MemoryService) Validate() error {
	if s.embeddingModel == "" {
		return errors.New("memory service: embedding model is empty")
	}
	if s.contradictionModel == "" {
		return errors.New("memory service: contradiction model is empty")
	}
	if s.dedupeUpper <= s.dedupeLower {
		return fmt.Errorf("memory service: dedupe_upper (%f) must exceed dedupe_lower (%f)", s.dedupeUpper, s.dedupeLower)
	}
	if s.dedupeUpper > 1 || s.dedupeLower < 0 {
		return fmt.Errorf("memory service: dedupe thresholds out of range [0,1]: upper=%f lower=%f", s.dedupeUpper, s.dedupeLower)
	}
	if s.hardCap <= 0 {
		return errors.New("memory service: hard_cap must be positive")
	}
	return nil
}

type RememberResult struct {
	ID           uuid.UUID
	CreatedAt    time.Time
	Deduped      bool
	SupersededID *uuid.UUID
}

type RecallResult struct {
	ID         uuid.UUID
	Content    string
	Tags       []string
	Score      float64
	Similarity float64
	CreatedAt  time.Time
}

type ForgetCandidate struct {
	ID         uuid.UUID
	Content    string
	Similarity float64
}

type ForgetResult struct {
	Forgotten  bool
	ID         *uuid.UUID
	Content    *string
	Reason     string // "" | "no_match" | "ambiguous"
	Candidates []ForgetCandidate
}

type ListParams struct {
	AssistantID     uuid.UUID
	Tags            []string
	IncludeDeleted  bool
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
	Limit           int32
}

type ListResult struct {
	Memories []repo.ListAssistantMemoriesForAdminRow
}

const (
	rememberOutcomeCreated    = "created"
	rememberOutcomeSuperseded = "superseded"
	rememberOutcomeDeduped    = "deduped"

	forgetOutcomeForgotten = "forgotten"
	forgetOutcomeNoMatch   = "no_match"
	forgetOutcomeAmbiguous = "ambiguous"

	forgetReasonToolForget       = "tool_forget"
	forgetReasonManualUUIDDelete = "manual_uuid_delete"
)

func (s *MemoryService) Remember(
	ctx context.Context,
	assistantID uuid.UUID,
	projectID uuid.UUID,
	organizationID string,
	content string,
	tags []string,
) (result RememberResult, err error) {
	ctx, span := s.tracer.Start(ctx, "memory.Remember", trace.WithAttributes(
		attr.AssistantID(assistantID.String()),
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	zero := RememberResult{ID: uuid.Nil, CreatedAt: time.Time{}, Deduped: false, SupersededID: nil}

	if assistantID == uuid.Nil {
		return zero, oops.E(oops.CodeBadRequest, nil, "assistant id is required").Log(ctx, s.logger)
	}
	if strings.TrimSpace(content) == "" {
		return zero, oops.E(oops.CodeBadRequest, nil, "memory content is required")
	}
	if len(content) > maxContentBytes {
		return zero, oops.E(oops.CodeRequestTooLarge, nil, "memory content exceeds %d bytes", maxContentBytes)
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return zero, oops.E(oops.CodeUnauthorized, nil, "missing auth context")
	}

	embedStart := time.Now()
	vectors, err := s.completions.CreateEmbeddings(ctx, organizationID, s.embeddingModel, []string{content}, openrouter.WithEmbeddingDimensions(embeddingDimensions))
	if duration := time.Since(embedStart).Seconds(); s.metrics.embedDuration != nil {
		s.metrics.embedDuration.Record(ctx, duration, metric.WithAttributes(
			attr.GenAIRequestModel(s.embeddingModel),
			attr.Outcome(o11y.OutcomeFromError(err)),
		))
	}
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "create memory embedding").Log(ctx, s.logger)
	}
	if len(vectors) != 1 {
		return zero, oops.E(oops.CodeUnexpected, nil, "embedding response had %d vectors, expected 1", len(vectors)).Log(ctx, s.logger)
	}
	embedding := pgvector_go.NewHalfVector(vectors[0])

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "begin remember transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txq := repo.New(tx)

	if err := txq.LockAssistantForMemoryWrite(ctx, assistantID.String()); err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "lock assistant for memory write").Log(ctx, s.logger)
	}

	count, err := txq.CountActiveAssistantMemories(ctx, assistantID)
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "count active memories").Log(ctx, s.logger)
	}
	if count >= int64(s.hardCap) {
		return zero, oops.E(oops.CodeConflict, nil, "memory cap reached for assistant; call forget first")
	}

	nearest, nearestErr := txq.GetNearestActiveAssistantMemory(ctx, repo.GetNearestActiveAssistantMemoryParams{
		QueryEmbedding: embedding,
		AssistantID:    assistantID,
	})
	hasNearest := nearestErr == nil
	if !hasNearest && !errors.Is(nearestErr, pgx.ErrNoRows) {
		return zero, oops.E(oops.CodeUnexpected, nearestErr, "find nearest memory").Log(ctx, s.logger)
	}

	supersedesID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	outcome := rememberOutcomeCreated

	if hasNearest {
		switch {
		case nearest.Similarity >= s.dedupeUpper:
			if err := tx.Commit(ctx); err != nil {
				return zero, oops.E(oops.CodeUnexpected, err, "commit dedupe").Log(ctx, s.logger)
			}
			s.recordRememberOutcome(ctx, rememberOutcomeDeduped)
			return RememberResult{
				ID:           nearest.ID,
				CreatedAt:    time.Time{},
				Deduped:      true,
				SupersededID: nil,
			}, nil
		case nearest.Similarity >= s.dedupeLower:
			contradicts, contradictErr := s.detectContradiction(ctx, organizationID, projectID.String(), nearest.Content, content)
			if contradictErr != nil {
				if s.metrics.contradictionFails != nil {
					s.metrics.contradictionFails.Add(ctx, 1, metric.WithAttributes(
						attr.OrganizationID(organizationID),
					))
				}
				s.logger.WarnContext(ctx, "contradiction detection failed; falling back to additive insert",
					attr.SlogError(contradictErr))
			} else if contradicts {
				if err := txq.MarkAssistantMemorySuperseded(ctx, nearest.ID); err != nil {
					return zero, oops.E(oops.CodeUnexpected, err, "mark memory superseded").Log(ctx, s.logger)
				}
				supersedesID = uuid.NullUUID{UUID: nearest.ID, Valid: true}
				outcome = rememberOutcomeSuperseded
			}
		}
	}

	originThread := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if principal, hasPrincipal := contextvalues.GetAssistantPrincipal(ctx); hasPrincipal && principal.ThreadID != uuid.Nil {
		originThread = uuid.NullUUID{UUID: principal.ThreadID, Valid: true}
	}

	insertTags := tags
	if insertTags == nil {
		insertTags = []string{}
	}

	inserted, err := txq.InsertAssistantMemory(ctx, repo.InsertAssistantMemoryParams{
		AssistantID:    assistantID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
		Content:        content,
		Embedding:      embedding,
		Tags:           insertTags,
		OriginThreadID: originThread,
		OriginChatID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SupersedesID:   supersedesID,
	})
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "insert memory").Log(ctx, s.logger)
	}

	threadID := uuid.Nil
	if originThread.Valid {
		threadID = originThread.UUID
	}

	if err := s.audit.LogAssistantMemoryCreate(ctx, tx, audit.LogAssistantMemoryCreateEvent{
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		MemoryID:         inserted.ID,
		AssistantID:      assistantID,
		ThreadID:         threadID,
		ContentPreview:   conv.TruncateString(content, contentPreviewMaxRunes),
	}); err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "log memory create audit").Log(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "commit remember transaction").Log(ctx, s.logger)
	}

	if outcome == rememberOutcomeSuperseded && s.metrics.supersedeDepth != nil {
		s.metrics.supersedeDepth.Record(ctx, 1, metric.WithAttributes(
			attr.OrganizationID(organizationID),
		))
	}
	s.recordRememberOutcome(ctx, outcome)

	var resultSupersedes *uuid.UUID
	if supersedesID.Valid {
		copied := supersedesID.UUID
		resultSupersedes = &copied
	}

	createdAt := inserted.CreatedAt.Time
	span.SetStatus(codes.Ok, "")
	return RememberResult{
		ID:           inserted.ID,
		CreatedAt:    createdAt,
		Deduped:      false,
		SupersededID: resultSupersedes,
	}, nil
}

func (s *MemoryService) recordRememberOutcome(ctx context.Context, outcome string) {
	if s.metrics.rememberOutcome == nil {
		return
	}
	s.metrics.rememberOutcome.Add(ctx, 1, metric.WithAttributes(
		attr.OutcomeKey.String(outcome),
	))
}

// Recall finds the nearest active memories for an assistant. Embedding errors
// degrade to an empty result so recall failure is a recall miss for the agent.
func (s *MemoryService) Recall(
	ctx context.Context,
	assistantID uuid.UUID,
	organizationID string,
	query string,
	limit int,
	tags []string,
) (results []RecallResult, err error) {
	ctx, span := s.tracer.Start(ctx, "memory.Recall", trace.WithAttributes(
		attr.AssistantID(assistantID.String()),
		attr.OrganizationID(organizationID),
	))
	start := time.Now()
	defer func() {
		if s.metrics.recallDuration != nil {
			s.metrics.recallDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
				attr.OrganizationID(organizationID),
				attr.Outcome(o11y.OutcomeFromError(err)),
			))
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if assistantID == uuid.Nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "assistant id is required")
	}

	if strings.TrimSpace(query) == "" {
		return []RecallResult{}, nil
	}

	if limit <= 0 {
		limit = defaultRecallLimit
	}
	if limit > maxRecallLimit {
		limit = maxRecallLimit
	}

	embedStart := time.Now()
	vectors, embedErr := s.completions.CreateEmbeddings(ctx, organizationID, s.embeddingModel, []string{query}, openrouter.WithEmbeddingDimensions(embeddingDimensions))
	if duration := time.Since(embedStart).Seconds(); s.metrics.embedDuration != nil {
		s.metrics.embedDuration.Record(ctx, duration, metric.WithAttributes(
			attr.GenAIRequestModel(s.embeddingModel),
			attr.Outcome(o11y.OutcomeFromError(embedErr)),
		))
	}
	if embedErr != nil {
		s.logger.WarnContext(ctx, "recall embedding failed; returning empty result",
			attr.SlogAssistantID(assistantID.String()),
			attr.SlogError(embedErr))
		return []RecallResult{}, nil
	}
	if len(vectors) != 1 {
		s.logger.WarnContext(ctx, "recall embedding returned wrong vector count; returning empty result")
		return []RecallResult{}, nil
	}

	queries := repo.New(s.db)

	tagFilter := tags
	if tagFilter == nil {
		tagFilter = []string{}
	}

	rows, err := queries.ListNearestAssistantMemories(ctx, repo.ListNearestAssistantMemoriesParams{
		QueryEmbedding: pgvector_go.NewHalfVector(vectors[0]),
		AssistantID:    assistantID,
		Tags:           tagFilter,
		ResultLimit:    int32(limit),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list nearest memories").Log(ctx, s.logger)
	}

	if len(rows) == 0 {
		return []RecallResult{}, nil
	}

	now := time.Now()
	scored := make([]RecallResult, 0, len(rows))
	for _, row := range rows {
		age := time.Duration(0)
		if row.LastAccess.Valid {
			age = now.Sub(row.LastAccess.Time)
		}
		scored = append(scored, RecallResult{
			ID:         row.ID,
			Content:    row.Content,
			Tags:       row.Tags,
			Similarity: row.Similarity,
			Score:      computeScore(row.Similarity, age, s.halfLife),
			CreatedAt:  row.CreatedAt.Time,
		})
	}

	sortByScoreDesc(scored)

	results = capAggregate(scored, s.perResultBytes, s.aggregateBytes, s.truncationSuffix)

	survivingIDs := make([]uuid.UUID, len(results))
	for i, r := range results {
		survivingIDs[i] = r.ID
	}

	if len(survivingIDs) > 0 {
		bumpCtx := context.WithoutCancel(ctx)
		bumpLogger := s.logger
		go func() {
			defer func() {
				if r := recover(); r != nil {
					bumpLogger.ErrorContext(bumpCtx, "panic in last_access bump",
						attr.SlogError(fmt.Errorf("%v", r)))
				}
			}()
			if err := repo.New(s.db).BumpAssistantMemoryLastAccess(bumpCtx, survivingIDs); err != nil {
				bumpLogger.WarnContext(bumpCtx, "bump last_access", attr.SlogError(err))
			}
		}()
	}

	span.SetStatus(codes.Ok, "")
	return results, nil
}

func (s *MemoryService) Forget(
	ctx context.Context,
	assistantID uuid.UUID,
	projectID uuid.UUID,
	organizationID string,
	query string,
	tags []string,
) (result ForgetResult, err error) {
	ctx, span := s.tracer.Start(ctx, "memory.Forget", trace.WithAttributes(
		attr.AssistantID(assistantID.String()),
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	zero := ForgetResult{Forgotten: false, ID: nil, Content: nil, Reason: "", Candidates: nil}

	if assistantID == uuid.Nil {
		return zero, oops.E(oops.CodeBadRequest, nil, "assistant id is required")
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return zero, oops.E(oops.CodeUnauthorized, nil, "missing auth context")
	}

	noMatch := ForgetResult{Forgotten: false, ID: nil, Content: nil, Reason: forgetOutcomeNoMatch, Candidates: nil}

	if strings.TrimSpace(query) == "" {
		s.recordForgetOutcome(ctx, forgetOutcomeNoMatch)
		return noMatch, nil
	}

	embedStart := time.Now()
	vectors, embedErr := s.completions.CreateEmbeddings(ctx, organizationID, s.embeddingModel, []string{query}, openrouter.WithEmbeddingDimensions(embeddingDimensions))
	if duration := time.Since(embedStart).Seconds(); s.metrics.embedDuration != nil {
		s.metrics.embedDuration.Record(ctx, duration, metric.WithAttributes(
			attr.GenAIRequestModel(s.embeddingModel),
			attr.Outcome(o11y.OutcomeFromError(embedErr)),
		))
	}
	if embedErr != nil {
		s.logger.WarnContext(ctx, "forget embedding failed; treating as no-match",
			attr.SlogError(embedErr))
		s.recordForgetOutcome(ctx, forgetOutcomeNoMatch)
		return noMatch, nil
	}
	if len(vectors) != 1 {
		s.recordForgetOutcome(ctx, forgetOutcomeNoMatch)
		return noMatch, nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "begin forget transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txq := repo.New(tx)

	if err := txq.LockAssistantForMemoryWrite(ctx, assistantID.String()); err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "lock assistant for forget").Log(ctx, s.logger)
	}

	tagFilter := tags
	if tagFilter == nil {
		tagFilter = []string{}
	}

	candidates, err := txq.ListNearestAssistantMemories(ctx, repo.ListNearestAssistantMemoriesParams{
		QueryEmbedding: pgvector_go.NewHalfVector(vectors[0]),
		AssistantID:    assistantID,
		Tags:           tagFilter,
		ResultLimit:    int32(forgetCandidateLimit),
	})
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "list forget candidates").Log(ctx, s.logger)
	}

	decision := decideForgetSelection(candidates, s.dedupeLower, s.forgetAmbiguityGap)
	switch decision.Outcome {
	case forgetOutcomeNoMatch:
		s.recordForgetOutcome(ctx, forgetOutcomeNoMatch)
		return noMatch, nil
	case forgetOutcomeAmbiguous:
		s.recordForgetOutcome(ctx, forgetOutcomeAmbiguous)
		return ForgetResult{
			Forgotten:  false,
			ID:         nil,
			Content:    nil,
			Reason:     forgetOutcomeAmbiguous,
			Candidates: decision.Candidates,
		}, nil
	}

	target := candidates[0]
	if _, err := txq.SoftDeleteAssistantMemory(ctx, target.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.recordForgetOutcome(ctx, forgetOutcomeNoMatch)
			return noMatch, nil
		}
		return zero, oops.E(oops.CodeUnexpected, err, "soft delete memory").Log(ctx, s.logger)
	}

	threadID := uuid.Nil
	if principal, hasPrincipal := contextvalues.GetAssistantPrincipal(ctx); hasPrincipal {
		threadID = principal.ThreadID
	}

	if err := s.audit.LogAssistantMemoryDelete(ctx, tx, audit.LogAssistantMemoryDeleteEvent{
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		MemoryID:         target.ID,
		AssistantID:      assistantID,
		ThreadID:         threadID,
		Reason:           forgetReasonToolForget,
	}); err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "log memory delete audit").Log(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "commit forget transaction").Log(ctx, s.logger)
	}

	id := target.ID
	content := target.Content
	s.recordForgetOutcome(ctx, forgetOutcomeForgotten)
	return ForgetResult{
		Forgotten:  true,
		ID:         &id,
		Content:    &content,
		Reason:     "",
		Candidates: nil,
	}, nil
}

type forgetDecision struct {
	Outcome    string
	Candidates []ForgetCandidate
}

const forgetOutcomeHit = "hit"

func decideForgetSelection(rows []repo.ListNearestAssistantMemoriesRow, minSimilarity, ambiguityGap float64) forgetDecision {
	if len(rows) == 0 || rows[0].Similarity < minSimilarity {
		return forgetDecision{Outcome: forgetOutcomeNoMatch, Candidates: nil}
	}
	if len(rows) >= 2 && (rows[0].Similarity-rows[1].Similarity) < ambiguityGap {
		out := make([]ForgetCandidate, 0, len(rows))
		for _, c := range rows {
			out = append(out, ForgetCandidate{
				ID:         c.ID,
				Content:    c.Content,
				Similarity: c.Similarity,
			})
		}
		return forgetDecision{Outcome: forgetOutcomeAmbiguous, Candidates: out}
	}
	return forgetDecision{Outcome: forgetOutcomeHit, Candidates: nil}
}

func (s *MemoryService) recordForgetOutcome(ctx context.Context, outcome string) {
	if s.metrics.forgetInvocations == nil {
		return
	}
	s.metrics.forgetInvocations.Add(ctx, 1, metric.WithAttributes(
		attr.OutcomeKey.String(outcome),
	))
}

func (s *MemoryService) DeleteByID(ctx context.Context, projectID uuid.UUID, id uuid.UUID) (err error) {
	ctx, span := s.tracer.Start(ctx, "memory.DeleteByID", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.E(oops.CodeUnauthorized, nil, "missing auth context")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin delete transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txq := repo.New(tx)

	deleted, err := txq.SoftDeleteAssistantMemoryByProject(ctx, repo.SoftDeleteAssistantMemoryByProjectParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "memory not found")
		}
		return oops.E(oops.CodeUnexpected, err, "soft delete memory").Log(ctx, s.logger)
	}

	if err := s.audit.LogAssistantMemoryDelete(ctx, tx, audit.LogAssistantMemoryDeleteEvent{
		OrganizationID:   deleted.OrganizationID,
		ProjectID:        deleted.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		MemoryID:         deleted.ID,
		AssistantID:      deleted.AssistantID,
		ThreadID:         uuid.Nil,
		Reason:           forgetReasonManualUUIDDelete,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log memory delete audit").Log(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit delete transaction").Log(ctx, s.logger)
	}

	return nil
}

func (s *MemoryService) List(ctx context.Context, projectID uuid.UUID, params ListParams) (ListResult, error) {
	queries := repo.New(s.db)

	tagFilter := params.Tags
	if tagFilter == nil {
		tagFilter = []string{}
	}

	cursor := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	if params.CursorCreatedAt != nil {
		cursor = pgtype.Timestamptz{Time: *params.CursorCreatedAt, Valid: true, InfinityModifier: pgtype.Finite}
	}
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if params.CursorID != nil {
		cursorID = uuid.NullUUID{UUID: *params.CursorID, Valid: true}
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}

	rows, err := queries.ListAssistantMemoriesForAdmin(ctx, repo.ListAssistantMemoriesForAdminParams{
		AssistantID:     params.AssistantID,
		ProjectID:       projectID,
		Tags:            tagFilter,
		IncludeDeleted:  params.IncludeDeleted,
		CursorCreatedAt: cursor,
		CursorID:        cursorID,
		PageLimit:       limit,
	})
	if err != nil {
		return ListResult{}, oops.E(oops.CodeUnexpected, err, "list memories").Log(ctx, s.logger)
	}

	return ListResult{Memories: rows}, nil
}

func (s *MemoryService) Get(ctx context.Context, projectID uuid.UUID, id uuid.UUID) (repo.GetAssistantMemoryByIDRow, error) {
	queries := repo.New(s.db)

	mem, err := queries.GetAssistantMemoryByID(ctx, repo.GetAssistantMemoryByIDParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.GetAssistantMemoryByIDRow{}, oops.E(oops.CodeNotFound, nil, "memory not found")
		}
		return repo.GetAssistantMemoryByIDRow{}, oops.E(oops.CodeUnexpected, err, "fetch memory").Log(ctx, s.logger)
	}

	return mem, nil
}

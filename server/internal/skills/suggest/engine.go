// Package suggest generates evidence-backed edits to recently active skills.
package suggest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type InsightsReader interface {
	QuerySkillInsights(context.Context, telemetryrepo.QuerySkillInsightsParams) ([]telemetryrepo.SkillInsightBucket, error)
}

type Engine struct {
	config    Config
	logger    *slog.Logger
	db        *pgxpool.Pool
	insights  InsightsReader
	chats     efficacy.TranscriptSource
	generator *modelGenerator
}

func NewEngine(config Config, logger *slog.Logger, db *pgxpool.Pool, insights InsightsReader, chats efficacy.TranscriptSource, completion CompletionClient, limiter *ratelimit.Limiter) (*Engine, error) {
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("suggestion config: %w", err)
	}
	if logger == nil || db == nil || insights == nil || chats == nil || completion == nil || limiter == nil {
		return nil, errors.New("suggestion engine dependencies cannot be nil")
	}
	return &Engine{
		config:    config,
		logger:    logger,
		db:        db,
		insights:  insights,
		chats:     chats,
		generator: &modelGenerator{config: config, logger: logger, completion: completion, limiter: limiter},
	}, nil
}

type RunInput struct {
	ProjectID uuid.UUID
	SkillID   uuid.UUID
	Now       time.Time
}

type ResultKind string

const (
	ResultSkipped   ResultKind = "skipped"
	ResultDeclined  ResultKind = "declined"
	ResultProposed  ResultKind = "proposed"
	ResultBaseMoved ResultKind = "base_moved"
)

type Result struct {
	Kind             ResultKind
	Reenqueue        bool
	FeedbackConsumed int64
	SuggestionID     uuid.NullUUID
}

type trend struct {
	CurrentCount    uint64  `json:"current_scored_sessions"`
	CurrentAverage  float64 `json:"current_average_score"`
	PreviousCount   uint64  `json:"predecessor_scored_sessions"`
	PreviousAverage float64 `json:"predecessor_average_score"`
	AbsoluteDelta   float64 `json:"absolute_delta"`
	Comparable      bool    `json:"comparable"`
	Regression      bool    `json:"regression"`
}

type wakeEvidence struct {
	now               time.Time
	lastSeenAt        time.Time
	floorReferenceAt  time.Time
	baseVersionID     uuid.UUID
	latest            *repo.SkillEditSuggestion
	unreviewedCount   int64
	newScoredSessions uint64
	trend             trend
}

type suggestionSnapshot struct {
	id            uuid.UUID
	status        string
	baseVersionID uuid.UUID
	updatedAt     time.Time
}

func (e *Engine) Run(ctx context.Context, in RunInput) (Result, error) {
	now := in.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	queries := repo.New(e.db)
	skill, err := queries.GetSkill(ctx, repo.GetSkillParams{ProjectID: in.ProjectID, ID: in.SkillID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{Kind: ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("get suggestion skill: %w", err)
	}
	if !skill.LastSeenAt.Valid || skill.LastSeenAt.Time.Before(now.Add(-e.config.ActivityWindow)) {
		return Result{Kind: ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}

	base, err := queries.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{ProjectID: in.ProjectID, SkillID: in.SkillID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{Kind: ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("resolve suggestion base: %w", err)
	}
	project, err := projectsrepo.New(e.db).GetProjectByID(ctx, in.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{Kind: ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("get suggestion project: %w", err)
	}
	unreviewedCount, err := queries.CountUnreviewedSkillFeedback(ctx, repo.CountUnreviewedSkillFeedbackParams{ProjectID: in.ProjectID, SkillName: skill.Name})
	if err != nil {
		return Result{}, fmt.Errorf("count suggestion feedback: %w", err)
	}
	feedback, err := queries.ListUnreviewedSkillFeedback(ctx, repo.ListUnreviewedSkillFeedbackParams{
		ProjectID: in.ProjectID,
		SkillName: skill.Name,
		PageLimit: e.config.MaxFeedback,
	})
	if err != nil {
		return Result{}, fmt.Errorf("list suggestion feedback: %w", err)
	}

	versionIDs := []string{base.BaseVersionID.String()}
	if base.PredecessorVersionID != uuid.Nil {
		versionIDs = append(versionIDs, base.PredecessorVersionID.String())
	}
	buckets, err := e.insights.QuerySkillInsights(ctx, telemetryrepo.QuerySkillInsightsParams{
		OrganizationID:  project.OrganizationID,
		ProjectID:       in.ProjectID.String(),
		SkillIDs:        []string{in.SkillID.String()},
		SkillVersionIDs: versionIDs,
		From:            now.Add(-e.config.TrendWindow),
		To:              now,
		IntervalSeconds: int64(e.config.TrendWindow.Seconds()),
	})
	if err != nil {
		return Result{}, fmt.Errorf("query suggestion skill insights: %w", err)
	}
	computedTrend := summarizeTrend(e.config, base, buckets)

	var latest *repo.SkillEditSuggestion
	latestRow, err := queries.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: in.ProjectID, SkillID: in.SkillID})
	switch {
	case err == nil:
		latest = &latestRow
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Result{}, fmt.Errorf("get latest skill suggestion: %w", err)
	}
	var newScoredSessions uint64
	if latest != nil && latest.BaseVersionID == base.BaseVersionID {
		count, err := queries.CountScoredSkillEvaluationsAfter(ctx, repo.CountScoredSkillEvaluationsAfterParams{
			ProjectID: in.ProjectID, SkillID: in.SkillID, BaseVersionID: base.BaseVersionID, ScoredAfter: latest.UpdatedAt,
		})
		if err != nil {
			return Result{}, fmt.Errorf("count new scored suggestion evaluations: %w", err)
		}
		newScoredSessions = nonnegativeInt64(count)
	}

	wake, _ := shouldWake(e.config, wakeEvidence{
		now: now, lastSeenAt: skill.LastSeenAt.Time, floorReferenceAt: base.BaseFloorReferenceAt.Time,
		baseVersionID: base.BaseVersionID, latest: latest, unreviewedCount: unreviewedCount,
		newScoredSessions: newScoredSessions, trend: computedTrend,
	})
	if !wake {
		return Result{Kind: ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}

	versionUUIDs := []uuid.UUID{base.BaseVersionID}
	if base.PredecessorVersionID != uuid.Nil {
		versionUUIDs = append(versionUUIDs, base.PredecessorVersionID)
	}
	chatRows, err := queries.ListRecentScoredSkillEvaluationChats(ctx, repo.ListRecentScoredSkillEvaluationChatsParams{
		ProjectID:   in.ProjectID,
		SkillID:     in.SkillID,
		VersionIds:  versionUUIDs,
		ScoredSince: pgtype.Timestamptz{Time: now.Add(-e.config.TranscriptWindow), InfinityModifier: pgtype.Finite, Valid: true},
		PageLimit:   e.config.MaxTranscripts,
	})
	if err != nil {
		return Result{}, fmt.Errorf("list suggestion transcripts: %w", err)
	}
	transcripts := make([]EvidenceTranscript, 0, len(chatRows))
	transcriptCache := make(map[uuid.UUID]struct{}, len(chatRows))
	for _, row := range chatRows {
		if len(transcripts) >= 3 {
			break
		}
		if _, ok := transcriptCache[row.ChatID]; ok {
			continue
		}
		transcript, err := efficacy.LoadTranscript(ctx, e.chats, in.ProjectID, row.ChatID)
		if err != nil {
			return Result{}, fmt.Errorf("load suggestion transcript: %w", err)
		}
		transcriptCache[row.ChatID] = struct{}{}
		transcripts = append(transcripts, EvidenceTranscript{
			Surface:        row.Surface,
			SkillVersionID: row.SkillVersionID,
			ScoredAt:       row.ScoredAt.Time,
			Transcript:     transcript,
		})
	}

	generation, err := e.generator.Generate(ctx, GenerateInput{
		OrganizationID:  project.OrganizationID,
		ProjectID:       in.ProjectID,
		SkillName:       skill.Name,
		Base:            base,
		Feedback:        feedback,
		Trend:           computedTrend,
		Transcripts:     transcripts,
		ValidationError: "",
	})
	if err != nil {
		return Result{}, err
	}

	if generation.Decision == DecisionPropose {
		validated, validationErr := skills.ValidateSkillSuggestion(generation.ProposedSkillMD, skill.Name, base.BaseCanonicalSha256)
		switch {
		case validationErr == nil:
			generation.ProposedSkillMD = validated.Content
		case errors.Is(validationErr, skills.ErrSkillSuggestionNoOp):
			generation = Generation{Decision: DecisionDecline, ProposedSkillMD: "", Rationale: "The proposed edit was canonically unchanged from the current skill."}
		default:
			generation, err = e.generator.Generate(ctx, GenerateInput{
				OrganizationID:  project.OrganizationID,
				ProjectID:       in.ProjectID,
				SkillName:       skill.Name,
				Base:            base,
				Feedback:        feedback,
				Trend:           computedTrend,
				Transcripts:     transcripts,
				ValidationError: validationErr.Error(),
			})
			if err != nil {
				return Result{}, err
			}
			if generation.Decision == DecisionPropose {
				validated, validationErr = skills.ValidateSkillSuggestion(generation.ProposedSkillMD, skill.Name, base.BaseCanonicalSha256)
				if validationErr != nil {
					e.logger.WarnContext(ctx, "discarding invalid corrected skill suggestion", attr.SlogError(validationErr), attr.SlogProjectID(in.ProjectID.String()), attr.SlogResourceID(in.SkillID.String()))
					generation = Generation{Decision: DecisionDecline, ProposedSkillMD: "", Rationale: "The proposed edit remained invalid after one correction attempt."}
				} else {
					generation.ProposedSkillMD = validated.Content
				}
			}
		}
	}

	rationale := evidenceRationale(e.config.MaxRationaleRunes, generation.Rationale, len(feedback), computedTrend)
	return e.persist(ctx, in, base.BaseVersionID, snapshotSuggestion(latest), skill.Name, generation, rationale, computedTrend.CurrentCount, unreviewedCount, feedback)
}

func summarizeTrend(config Config, base repo.ResolveSkillSuggestionBaseRow, buckets []telemetryrepo.SkillInsightBucket) trend {
	var currentScore, previousScore float64
	result := trend{
		CurrentCount:    0,
		CurrentAverage:  0,
		PreviousCount:   0,
		PreviousAverage: 0,
		AbsoluteDelta:   0,
		Comparable:      false,
		Regression:      false,
	}
	for _, bucket := range buckets {
		switch bucket.SkillVersionID {
		case base.BaseVersionID.String():
			result.CurrentCount += bucket.ScoredSessions
			currentScore += bucket.ScoreSum
		case base.PredecessorVersionID.String():
			result.PreviousCount += bucket.ScoredSessions
			previousScore += bucket.ScoreSum
		}
	}
	if result.CurrentCount > 0 {
		result.CurrentAverage = currentScore / float64(result.CurrentCount)
	}
	if result.PreviousCount > 0 {
		result.PreviousAverage = previousScore / float64(result.PreviousCount)
	}
	result.AbsoluteDelta = result.PreviousAverage - result.CurrentAverage
	result.Comparable = base.PredecessorVersionID != uuid.Nil && result.CurrentCount >= config.MinRegressionScoredSessions && result.PreviousCount >= config.MinRegressionScoredSessions
	result.Regression = result.Comparable && result.AbsoluteDelta+1e-9 >= config.RegressionAbsoluteDelta
	return result
}

func shouldWake(config Config, evidence wakeEvidence) (bool, string) {
	if evidence.lastSeenAt.Before(evidence.now.Add(-config.ActivityWindow)) {
		return false, "dormant"
	}
	if evidence.unreviewedCount >= int64(config.MinUnreviewedFeedback) {
		return true, "feedback"
	}

	latest := evidence.latest
	sameBase := latest != nil && latest.BaseVersionID == evidence.baseVersionID
	if !sameBase && evidence.trend.Regression {
		return true, "regression"
	}
	if !sameBase {
		if evidence.unreviewedCount == 0 && evidence.trend.CurrentCount < config.MinAdditionalScoredSessions {
			return false, "insufficient_evidence"
		}
		if evidence.now.Sub(evidence.floorReferenceAt) < config.SuggestionFloor {
			return false, "weekly_floor"
		}
		return true, "base_reset"
	}

	dismissed := latest.Status == string(skills.EditSuggestionStatusDismissed)
	requiredScoredAdvance := config.MinAdditionalScoredSessions
	if dismissed {
		requiredScoredAdvance = config.DismissedScoredAdvance
	}
	scoredEvidence := evidence.newScoredSessions >= requiredScoredAdvance
	if evidence.trend.Regression && scoredEvidence {
		return true, "regression"
	}
	if !scoredEvidence && (dismissed || evidence.unreviewedCount == 0) {
		return false, "no_new_evidence"
	}
	if evidence.now.Sub(latest.UpdatedAt.Time) < config.SuggestionFloor {
		return false, "weekly_floor"
	}
	return true, "weekly_floor"
}

func evidenceRationale(maxRunes int, modelRationale string, feedbackCount int, evidence trend) string {
	prefix := fmt.Sprintf("**Evidence:** feedback=%d; current-base scored sessions=%d; predecessor scored sessions=%d.\n\n", feedbackCount, evidence.CurrentCount, evidence.PreviousCount)
	remaining := maxRunes - utf8.RuneCountInString(prefix)
	if remaining <= 0 {
		return string([]rune(prefix)[:maxRunes])
	}
	rationale := strings.TrimSpace(modelRationale)
	if utf8.RuneCountInString(rationale) > remaining {
		rationale = string([]rune(rationale)[:remaining])
	}
	return prefix + rationale
}

func snapshotSuggestion(suggestion *repo.SkillEditSuggestion) *suggestionSnapshot {
	if suggestion == nil {
		return nil
	}
	return &suggestionSnapshot{
		id: suggestion.ID, status: suggestion.Status, baseVersionID: suggestion.BaseVersionID,
		updatedAt: suggestion.UpdatedAt.Time,
	}
}

func suggestionSnapshotMatches(expected *suggestionSnapshot, actual *repo.SkillEditSuggestion) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	return expected.id == actual.ID && expected.status == actual.Status && expected.baseVersionID == actual.BaseVersionID && expected.updatedAt.Equal(actual.UpdatedAt.Time)
}

func (e *Engine) persist(ctx context.Context, in RunInput, expectedBase uuid.UUID, expectedLatest *suggestionSnapshot, skillName string, generation Generation, rationale string, scoredCount uint64, totalUnreviewed int64, feedback []repo.SkillFeedback) (Result, error) {
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin suggestion transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	if _, err := queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{ProjectID: in.ProjectID, ID: in.SkillID}); errors.Is(err, pgx.ErrNoRows) {
		return Result{Kind: ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	} else if err != nil {
		return Result{}, fmt.Errorf("lock suggestion skill: %w", err)
	}
	base, err := queries.ResolveSkillSuggestionBase(ctx, repo.ResolveSkillSuggestionBaseParams{ProjectID: in.ProjectID, SkillID: in.SkillID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{Kind: ResultSkipped, Reenqueue: false, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("re-resolve suggestion base: %w", err)
	}
	if base.BaseVersionID != expectedBase {
		return Result{Kind: ResultBaseMoved, Reenqueue: true, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}
	var latest *repo.SkillEditSuggestion
	latestRow, err := queries.GetLatestSkillEditSuggestion(ctx, repo.GetLatestSkillEditSuggestionParams{ProjectID: in.ProjectID, SkillID: in.SkillID})
	switch {
	case err == nil:
		latest = &latestRow
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Result{}, fmt.Errorf("re-read latest skill suggestion: %w", err)
	}
	dismissedDuringInference := expectedLatest != nil && latest != nil &&
		expectedLatest.id == latest.ID && expectedLatest.status == string(skills.EditSuggestionStatusOpen) &&
		latest.Status == string(skills.EditSuggestionStatusDismissed) && expectedLatest.baseVersionID == latest.BaseVersionID
	if !suggestionSnapshotMatches(expectedLatest, latest) && !dismissedDuringInference {
		return Result{Kind: ResultBaseMoved, Reenqueue: true, FeedbackConsumed: 0, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}
	ids := make([]uuid.UUID, len(feedback))
	for i := range feedback {
		ids[i] = feedback[i].ID
	}
	var consumed int64
	if len(ids) > 0 {
		consumed, err = queries.MarkSkillFeedbackReviewed(ctx, repo.MarkSkillFeedbackReviewedParams{ProjectID: in.ProjectID, SkillName: skillName, Ids: ids})
		if err != nil {
			return Result{}, fmt.Errorf("consume skill suggestion feedback: %w", err)
		}
	}
	if dismissedDuringInference {
		if _, err := queries.UpdateLatestSkillEditSuggestionWatermark(ctx, repo.UpdateLatestSkillEditSuggestionWatermarkParams{
			FeedbackCount: consumed, ScoredSessionCount: clampUint64ToInt64(scoredCount),
			ProjectID: in.ProjectID, SkillID: in.SkillID, BaseVersionID: expectedBase,
		}); err != nil {
			return Result{}, fmt.Errorf("update dismissed skill suggestion watermark: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Result{}, fmt.Errorf("commit dismissed skill suggestion watermark: %w", err)
		}
		return Result{Kind: ResultDeclined, Reenqueue: false, FeedbackConsumed: consumed, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}

	suggestionID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if generation.Decision == DecisionPropose {
		open, openErr := queries.GetOpenSkillEditSuggestion(ctx, repo.GetOpenSkillEditSuggestionParams{ProjectID: in.ProjectID, SkillID: in.SkillID})
		switch {
		case openErr == nil && open.BaseVersionID == expectedBase:
			updated, err := queries.UpdateOpenSkillEditSuggestion(ctx, repo.UpdateOpenSkillEditSuggestionParams{
				ProposedContent: generation.ProposedSkillMD, Rationale: rationale, FeedbackCount: consumed,
				ScoredSessionCount: clampUint64ToInt64(scoredCount), ProjectID: in.ProjectID, SkillID: in.SkillID, BaseVersionID: expectedBase,
			})
			if err != nil {
				return Result{}, fmt.Errorf("update open skill suggestion: %w", err)
			}
			suggestionID = uuid.NullUUID{UUID: updated.ID, Valid: true}
		case openErr == nil:
			if _, err := queries.SupersedeOpenSkillEditSuggestion(ctx, repo.SupersedeOpenSkillEditSuggestionParams{ProjectID: in.ProjectID, SkillID: in.SkillID, CurrentBaseVersionID: expectedBase}); err != nil {
				return Result{}, fmt.Errorf("supersede stale skill suggestion: %w", err)
			}
		case errors.Is(openErr, pgx.ErrNoRows):
		case openErr != nil:
			return Result{}, fmt.Errorf("get open skill suggestion: %w", openErr)
		}
		if !suggestionID.Valid {
			created, err := queries.CreateSkillEditSuggestion(ctx, repo.CreateSkillEditSuggestionParams{
				ProposedContent: generation.ProposedSkillMD, Rationale: rationale, FeedbackCount: consumed,
				ScoredSessionCount: clampUint64ToInt64(scoredCount), BaseVersionID: expectedBase, ProjectID: in.ProjectID, SkillID: in.SkillID,
			})
			if err != nil {
				return Result{}, fmt.Errorf("create skill suggestion: %w", err)
			}
			suggestionID = uuid.NullUUID{UUID: created.ID, Valid: true}
		}
	} else if latest != nil && latest.BaseVersionID == expectedBase {
		if _, err := queries.UpdateLatestSkillEditSuggestionWatermark(ctx, repo.UpdateLatestSkillEditSuggestionWatermarkParams{
			FeedbackCount: consumed, ScoredSessionCount: clampUint64ToInt64(scoredCount),
			ProjectID: in.ProjectID, SkillID: in.SkillID, BaseVersionID: expectedBase,
		}); err != nil {
			return Result{}, fmt.Errorf("update skill suggestion watermark: %w", err)
		}
	} else {
		if _, err := queries.CreateSkillEditSuggestionWatermark(ctx, repo.CreateSkillEditSuggestionWatermarkParams{
			Rationale: "Automated analysis completed without a proposed edit.", FeedbackCount: consumed,
			ScoredSessionCount: clampUint64ToInt64(scoredCount), BaseVersionID: expectedBase,
			ProjectID: in.ProjectID, SkillID: in.SkillID,
		}); err != nil {
			return Result{}, fmt.Errorf("create skill suggestion watermark: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit skill suggestion: %w", err)
	}
	reenqueue := totalUnreviewed > consumed
	if !suggestionID.Valid {
		return Result{Kind: ResultDeclined, Reenqueue: reenqueue, FeedbackConsumed: consumed, SuggestionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	}
	return Result{Kind: ResultProposed, Reenqueue: reenqueue, FeedbackConsumed: consumed, SuggestionID: suggestionID}, nil
}

func nonnegativeInt64(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func clampUint64ToInt64(value uint64) int64 {
	const maxInt64 = ^uint64(0) >> 1
	if value > maxInt64 {
		return int64(maxInt64)
	}
	return int64(value)
}

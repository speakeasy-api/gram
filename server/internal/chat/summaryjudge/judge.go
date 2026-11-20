// Package summaryjudge writes the masked chat summary that team memory and the
// dashboard share, as a scoreless judge on the chat analysis pipeline. The
// pipeline supplies the idle trigger, the per-organization opt-in and daily
// cap, retries and crash recovery.
package summaryjudge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/memorygate"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// JudgeName keys the judge's queue and spend rows. Summaries run for every
// organization that enabled the business_memory judge (see OptInWith); a
// chat_analysis_settings row with this name overrides that.
const JudgeName = "chat_summary"

// promptVersion tracks the masked summary prompt in chat.SummaryGenerator.
const promptVersion = "v1"

// outcome is the judge's verdict detail. Nothing reads it: the judge is
// scoreless, so the detail only reaches logs and tests.
type outcome struct {
	Outcome string `json:"outcome"`
}

const (
	outcomeWritten   = "written"
	outcomeEmpty     = "empty"
	outcomeSkipped   = "skipped"
	outcomeMemoryOff = "memory_off"
	outcomeNotFound  = "not_found"
)

// Judge writes a chat's masked summary once the chat has gone quiet, and again
// each time the chat resumes and goes quiet again.
type Judge struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	summaries *chat.SummaryGenerator
	client    openrouter.CompletionClient
	limiter   *ratelimit.Limiter
}

var (
	_ analysis.Judge            = (*Judge)(nil)
	_ analysis.ScorelessJudge   = (*Judge)(nil)
	_ analysis.RearmingJudge    = (*Judge)(nil)
	_ analysis.SharedOptInJudge = (*Judge)(nil)
)

// NewJudge returns a summary judge. It needs a completion client; with none the
// roster should leave it out.
func NewJudge(logger *slog.Logger, db *pgxpool.Pool, client openrouter.CompletionClient, limiter *ratelimit.Limiter) *Judge {
	summaries := chat.NewSummaryGenerator(db, client, chatredact.New())
	return &Judge{
		logger:    logger.With(attr.SlogComponent("chat-summary-judge")),
		db:        db,
		summaries: summaries.WithUsage(billing.ModelUsageSourceChatAnalysis, billing.ModelUsageSourceChatAnalysis),
		client:    client,
		limiter:   limiter,
	}
}

func (j *Judge) Name() string { return JudgeName }

func (j *Judge) SkipScoreSink() bool { return true }

func (j *Judge) RearmOnNewMessages() bool { return true }

// OptInWith shares the memory fact judge's opt-in and daily cap until the two
// settings fold into one Organizational Memory setting.
func (j *Judge) OptInWith() string { return businessmemory.JudgeName }

func (j *Judge) Judge(ctx context.Context, in analysis.JudgeInput) (analysis.JudgeResult, error) {
	projectID, err := uuid.Parse(in.ProjectID)
	if err != nil {
		return analysis.JudgeResult{}, fmt.Errorf("parse chat summary project id: %w", err)
	}

	// The settings row is the budget; the staff admin Business Memory
	// Extraction toggle is the Organizational Memory gate. An organization
	// with a summary row of its own but the toggle off spends nothing. A
	// failed lookup is retried rather than scored as off: a scored chat only
	// comes back once it has new messages.
	if j.summaries == nil {
		return j.finish(ctx, in, projectID, outcomeMemoryOff, nil)
	}
	switch on, err := memorygate.Enabled(ctx, j.db, in.OrgID); {
	case err != nil:
		return analysis.JudgeResult{}, fmt.Errorf("read organizational memory setting: %w: %w", analysis.ErrRetryable, err)
	case !on:
		return j.finish(ctx, in, projectID, outcomeMemoryOff, nil)
	}

	eligible, err := repo.New(j.db).GetChatMemorySummaryEligibility(ctx, repo.GetChatMemorySummaryEligibilityParams{ID: in.ChatID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return j.finish(ctx, in, projectID, outcomeNotFound, nil)
	case err != nil:
		return analysis.JudgeResult{}, fmt.Errorf("read chat summary eligibility: %w: %w", analysis.ErrRetryable, err)
	case !eligible:
		return j.finish(ctx, in, projectID, outcomeSkipped, nil)
	}

	if err := j.allow(ctx, in); err != nil {
		return analysis.JudgeResult{}, err
	}

	draft, err := j.summaries.Draft(ctx, in.OrgID, projectID, in.ChatID)
	switch {
	case errors.Is(err, chat.ErrNothingToSummarize):
		return j.finish(ctx, in, projectID, outcomeEmpty, nil)
	case err != nil:
		return analysis.JudgeResult{}, classify(ctx, err)
	}
	return j.finish(ctx, in, projectID, outcomeWritten, &draft)
}

// allow draws on the shared judge rate limiter. A limiter outage proceeds, a
// throttle is retryable, matching analysis.CallStructured.
func (j *Judge) allow(ctx context.Context, in analysis.JudgeInput) error {
	if j.limiter == nil {
		return nil
	}
	bucket := openrouter.ResolveJudgeRateLimitKey(ctx, j.logger, j.client, in.OrgID, in.ProjectID, billing.ModelUsageSourceChatAnalysis, j.summaries.Model())
	res, err := j.limiter.Allow(ctx, bucket)
	switch {
	case err != nil:
		j.logger.WarnContext(ctx, "judge rate limiter unavailable, allowing call", attr.SlogError(err), attr.SlogOrganizationID(in.OrgID))
		return nil
	case !res.Allowed:
		return fmt.Errorf("chat summary judge call: %w", analysis.ErrRetryable)
	}
	return nil
}

// classify sorts a summary generation failure the way the publisher expects:
// failures the model owns are charged to the attempt budget, the rest retried.
func classify(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, openrouter.ErrRateLimited):
		return fmt.Errorf("generate chat summary: %w", err)
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil,
		errors.Is(err, chat.ErrEmptySummary),
		openrouter.IsHistoryCorruptionCandidate(err),
		openrouter.IsBadRequest(err),
		openrouter.IsContentPolicy(err):
		return fmt.Errorf("generate chat summary: %w: %w", analysis.ErrModelFailure, err)
	default:
		return fmt.Errorf("generate chat summary: %w: %w", analysis.ErrRetryable, err)
	}
}

// finish stores the summary (or the empty marker) and marks the evaluation
// scored in one transaction, so a retry after a crash never double-writes and
// a scored evaluation always has its summary.
func (j *Judge) finish(ctx context.Context, in analysis.JudgeInput, projectID uuid.UUID, result string, draft *chat.SummaryDraft) (analysis.JudgeResult, error) {
	tx, err := j.db.Begin(ctx)
	if err != nil {
		return analysis.JudgeResult{}, fmt.Errorf("begin chat summary write: %w: %w", analysis.ErrRetryable, err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	chats := repo.New(tx)
	switch {
	case draft != nil:
		_, err = chats.UpdateChatSummary(ctx, repo.UpdateChatSummaryParams{
			Summary:         pgtype.Text{String: draft.Summary, Valid: true},
			SummaryRedacted: true,
			CoveredThrough:  draft.CoveredThrough,
			ID:              in.ChatID,
			ProjectID:       projectID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// The chat was deleted while the summary was written.
			result, err = outcomeNotFound, nil
		}
	case result == outcomeEmpty:
		err = chats.MarkChatSummaryEmpty(ctx, repo.MarkChatSummaryEmptyParams{ID: in.ChatID, ProjectID: projectID})
	}
	if err != nil {
		return analysis.JudgeResult{}, fmt.Errorf("store chat summary: %w: %w", analysis.ErrRetryable, err)
	}

	marked, err := analysisrepo.New(tx).MarkChatAnalysisEvaluationScored(ctx, analysisrepo.MarkChatAnalysisEvaluationScoredParams{
		ProjectID: projectID,
		ID:        in.EvaluationID,
	})
	if err != nil {
		return analysis.JudgeResult{}, fmt.Errorf("complete chat summary evaluation: %w: %w", analysis.ErrRetryable, err)
	}
	if marked != 1 {
		return analysis.JudgeResult{}, fmt.Errorf("complete chat summary evaluation: expected one reserved evaluation, updated %d: %w", marked, analysis.ErrRetryable)
	}
	if err := tx.Commit(ctx); err != nil {
		return analysis.JudgeResult{}, fmt.Errorf("commit chat summary: %w: %w", analysis.ErrRetryable, err)
	}

	detail, err := json.Marshal(outcome{Outcome: result})
	if err != nil {
		return analysis.JudgeResult{}, fmt.Errorf("marshal chat summary outcome: %w", err)
	}
	score := 0.0
	if result == outcomeWritten {
		score = 1
	}
	return analysis.JudgeResult{
		Verdict:       analysis.Verdict{Score: score, Detail: detail},
		Model:         j.summaries.Model(),
		PromptVersion: promptVersion,
	}, nil
}

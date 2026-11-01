package activities

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// MemorySummaryIdle is how long a chat must be quiet before the sweep
	// summarizes it. A chat that picks up again is summarized again the next
	// time it goes quiet.
	MemorySummaryIdle = 30 * time.Minute
	// MemorySummaryLookback bounds how far back the sweep looks. Chats from
	// before an organization's flag turned on are picked up newest first,
	// which makes the sweep double as the 90-day backfill.
	MemorySummaryLookback = 90 * 24 * time.Hour
	// MemorySummaryDailyCap is the most masked summaries the sweep writes for
	// one organization per UTC day. Dashboard Summarize clicks also count.
	MemorySummaryDailyCap = 2000
	// memorySummaryOrgBatch and memorySummaryTickBatch bound one sweep run
	// per organization and in total, so a large backfill spreads over runs.
	memorySummaryOrgBatch  = 200
	memorySummaryTickBatch = 1000
	// memorySummaryConcurrency bounds parallel model calls in one run.
	memorySummaryConcurrency = 8
)

// SummarizeIdleChatsArgs bounds one sweep run.
type SummarizeIdleChatsArgs struct {
	// Deadline is when the run stops starting new summaries, leaving room
	// under the activity timeout for calls already in flight.
	Deadline time.Time
}

// SummarizeIdleChatsResult reports one sweep run.
type SummarizeIdleChatsResult struct {
	Organizations int
	Summarized    int
	Empty         int
	Failed        int
	Capped        int
}

// SummarizeIdleChats writes masked summaries for chats that went quiet, for
// organizations with the team-memory flag on.
type SummarizeIdleChats struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	features  feature.Provider
	summaries *chat.SummaryGenerator
	now       func() time.Time
}

func NewSummarizeIdleChats(logger *slog.Logger, db *pgxpool.Pool, features feature.Provider, client openrouter.CompletionClient) *SummarizeIdleChats {
	return &SummarizeIdleChats{
		logger:    logger.With(attr.SlogComponent("summarize-idle-chats")),
		db:        db,
		repo:      repo.New(db),
		features:  features,
		summaries: chat.NewSummaryGenerator(db, client, chatredact.New()),
		now:       time.Now,
	}
}

type dueChat struct {
	orgID     string
	projectID uuid.UUID
	chatID    uuid.UUID
}

func (s *SummarizeIdleChats) Do(ctx context.Context, args SummarizeIdleChatsArgs) (*SummarizeIdleChatsResult, error) {
	result := &SummarizeIdleChatsResult{Organizations: 0, Summarized: 0, Empty: 0, Failed: 0, Capped: 0}
	if s.summaries == nil || s.features == nil {
		return result, nil
	}

	due, err := s.collect(ctx, result)
	if err != nil {
		return result, err
	}

	heartbeat := func() {}
	if activity.IsActivity(ctx) {
		heartbeat = func() { activity.RecordHeartbeat(ctx) }
	}

	var mu sync.Mutex
	slots := make(chan struct{}, memorySummaryConcurrency)
	var wg sync.WaitGroup
	for _, c := range due {
		if !args.Deadline.IsZero() && s.now().After(args.Deadline) {
			break
		}
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wg.Add(1)
		go func(c dueChat) {
			defer wg.Done()
			defer func() { <-slots }()
			outcome := s.summarize(ctx, c)
			mu.Lock()
			defer mu.Unlock()
			switch outcome {
			case summaryWritten:
				result.Summarized++
			case summaryEmpty:
				result.Empty++
			case summaryFailed:
				result.Failed++
			}
		}(c)
		heartbeat()
	}
	wg.Wait()

	s.logger.InfoContext(ctx, "team memory summary sweep finished",
		attr.SlogMemorySummaryOrganizationCount(result.Organizations),
		attr.SlogMemorySummaryWrittenCount(result.Summarized),
		attr.SlogMemorySummaryEmptyCount(result.Empty),
		attr.SlogMemorySummaryFailedCount(result.Failed),
		attr.SlogMemorySummaryCappedCount(result.Capped),
	)
	return result, nil
}

// collect lists due chats across flagged organizations, honoring the
// per-organization daily cap and the per-run limits.
func (s *SummarizeIdleChats) collect(ctx context.Context, result *SummarizeIdleChatsResult) ([]dueChat, error) {
	orgs, err := s.repo.ListOrganizationsWithChatsForMemorySweep(ctx)
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var due []dueChat
	for _, orgID := range orgs {
		if len(due) >= memorySummaryTickBatch {
			break
		}
		if !chat.TeamMemoryEnabled(ctx, s.logger, s.db, s.features, orgID) {
			continue
		}
		result.Organizations++

		written, err := s.repo.CountMemorySummariesSince(ctx, repo.CountMemorySummariesSinceParams{
			OrganizationID: orgID,
			Since:          pgtype.Timestamptz{Time: dayStart, InfinityModifier: pgtype.Finite, Valid: true},
		})
		if err != nil {
			return nil, err
		}
		budget := min(int64(MemorySummaryDailyCap)-written, memorySummaryOrgBatch, int64(memorySummaryTickBatch-len(due)))
		if budget <= 0 {
			result.Capped++
			continue
		}

		rows, err := s.repo.ListChatsDueForMemorySummary(ctx, repo.ListChatsDueForMemorySummaryParams{
			OrganizationID: orgID,
			IdleBefore:     pgtype.Timestamptz{Time: now.Add(-MemorySummaryIdle), InfinityModifier: pgtype.Finite, Valid: true},
			Since:          pgtype.Timestamptz{Time: now.Add(-MemorySummaryLookback), InfinityModifier: pgtype.Finite, Valid: true},
			MaxChats:       int32(budget), //nolint:gosec // bounded by memorySummaryOrgBatch
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			due = append(due, dueChat{orgID: orgID, projectID: row.ProjectID, chatID: row.ID})
		}
	}
	return due, nil
}

type summaryOutcome int

const (
	summaryWritten summaryOutcome = iota
	summaryEmpty
	summaryFailed
)

func (s *SummarizeIdleChats) summarize(ctx context.Context, c dueChat) summaryOutcome {
	_, err := s.summaries.Generate(ctx, c.orgID, c.projectID, c.chatID)
	switch {
	case err == nil:
		return summaryWritten
	case errors.Is(err, chat.ErrNothingToSummarize):
		if markErr := s.repo.MarkChatSummaryEmpty(ctx, repo.MarkChatSummaryEmptyParams{ID: c.chatID, ProjectID: c.projectID}); markErr != nil {
			s.logger.WarnContext(ctx, "mark empty chat summary", attr.SlogError(markErr), attr.SlogChatID(c.chatID.String()))
			return summaryFailed
		}
		return summaryEmpty
	case errors.Is(err, chat.ErrSummaryChatNotFound):
		return summaryEmpty
	default:
		s.logger.WarnContext(ctx, "generate masked chat summary", attr.SlogError(err), attr.SlogChatID(c.chatID.String()), attr.SlogOrganizationID(c.orgID))
		return summaryFailed
	}
}

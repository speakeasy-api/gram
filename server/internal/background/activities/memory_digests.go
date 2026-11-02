package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/memorydigest"
	"github.com/speakeasy-api/gram/server/internal/memorydigest/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// MemoryDigestModel writes day, week and month digests. Digests are a
	// few calls per active project per day that synthesize many summaries,
	// so a mid-tier model fits; the memory eval confirms or replaces it.
	MemoryDigestModel = "anthropic/claude-sonnet-5.5"

	// memoryDigestMaxOutputTokens caps one digest. The longest word budget
	// is 250 words, about 330 tokens; the cap leaves room for a model that
	// overruns without letting it run away.
	memoryDigestMaxOutputTokens = 1_000

	// memoryDigestCompletionTimeout bounds one digest call. Inputs reach
	// about 30k tokens on a busy day, which a mid-tier model reads well
	// inside a minute.
	memoryDigestCompletionTimeout = 90 * time.Second

	// MemoryDigestLookback is how far back the job looks for periods
	// without a digest. It covers the 90-day summary backfill an org gets
	// when the flag turns on, plus margin for the month that straddles it.
	MemoryDigestLookback = 100 * 24 * time.Hour

	// memoryDigestBatchSize is how many digests one activity writes. At
	// memoryDigestConcurrency calls in flight and under a minute each, a
	// batch finishes in about ten minutes.
	memoryDigestBatchSize = 40

	// memoryDigestConcurrency bounds parallel digest calls per activity.
	memoryDigestConcurrency = 4

	// maxDayDigestChats and maxDayDigestMemos bound one day's inputs. The
	// prompt keeps the newest material anyway; these stop the read from
	// loading a pathological day into memory.
	maxDayDigestChats = 400
	maxDayDigestMemos = 200
)

// MemoryDigests writes team memory digests for closed periods.
type MemoryDigests struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	repo       *repo.Queries
	chatClient openrouter.CompletionClient
	flags      feature.Provider
	model      string
}

func NewMemoryDigests(logger *slog.Logger, db *pgxpool.Pool, chatClient openrouter.CompletionClient, flags feature.Provider) *MemoryDigests {
	return &MemoryDigests{
		logger:     logger.With(attr.SlogComponent("memory_digests")),
		db:         db,
		repo:       repo.New(db),
		chatClient: chatClient,
		flags:      flags,
		model:      MemoryDigestModel,
	}
}

// WithModel returns a copy that writes digests with model. The team memory
// eval uses it to compare digest models; the scheduled job always uses
// MemoryDigestModel.
func (m *MemoryDigests) WithModel(model string) *MemoryDigests {
	copied := *m
	copied.model = model
	return &copied
}

// BuildMemoryDigestsArgs selects one level of one rollup run.
type BuildMemoryDigestsArgs struct {
	// Level is "day", "week" or "month".
	Level string

	// Now is the run's anchor time, so every batch of a run agrees on which
	// periods are closed.
	Now time.Time
}

// BuildMemoryDigestsResult counts what one batch did.
type BuildMemoryDigestsResult struct {
	// Built is the number of digests written.
	Built int

	// Skipped counts periods left alone because they had nothing to fold
	// or another run wrote them first.
	Skipped int

	// Failed counts periods whose digest could not be written.
	Failed int

	// More is true when candidates remain beyond this batch.
	More bool
}

type memoryDigestCandidate struct {
	organizationID string
	projectID      uuid.UUID
	periodStart    time.Time
}

// Build writes up to one batch of missing digests at args.Level. Per-period
// failures are logged and counted rather than returned, so one bad project
// cannot hold back the rest; the workflow decides whether higher levels may
// run on top of a level that had failures.
func (m *MemoryDigests) Build(ctx context.Context, args BuildMemoryDigestsArgs) (BuildMemoryDigestsResult, error) {
	var result BuildMemoryDigestsResult

	level, err := memorydigest.ParseLevel(args.Level)
	if err != nil {
		return result, fmt.Errorf("parse digest level: %w", err)
	}

	// The memory eval calls Build outside an activity, where there is no
	// heartbeat to record.
	if activity.IsActivity(ctx) {
		stopHeartbeat := startActivityHeartbeat(ctx)
		defer stopHeartbeat()
	}

	// Invalidation runs before anything else and for every organization, so
	// a source removed from memory leaves every digest that folded it even
	// when the flag is off or the run stops before higher levels.
	if err := m.invalidate(ctx); err != nil {
		return result, err
	}

	now := args.Now.UTC()
	orgIDs, err := m.enabledOrganizations(ctx, now)
	if err != nil {
		return result, err
	}
	if len(orgIDs) == 0 {
		return result, nil
	}

	candidates, err := m.listCandidates(ctx, level, now, orgIDs)
	if err != nil {
		return result, err
	}
	if len(candidates) > memoryDigestBatchSize {
		candidates = candidates[:memoryDigestBatchSize]
		result.More = true
	}

	var built, skipped, failed atomic.Int64
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(memoryDigestConcurrency)
	for _, c := range candidates {
		group.Go(func() error {
			wrote, err := m.buildOne(groupCtx, level, c)
			switch {
			case err != nil:
				failed.Add(1)
				m.logger.ErrorContext(groupCtx, "build memory digest",
					attr.SlogError(err),
					attr.SlogOrganizationID(c.organizationID),
					attr.SlogProjectID(c.projectID.String()),
				)
			case wrote:
				built.Add(1)
			default:
				skipped.Add(1)
			}
			return nil
		})
	}
	_ = group.Wait() // Workers never return errors; failures are counted.
	// A cancelled or timed-out attempt is not a set of per-period failures:
	// return the error so Temporal retries it. Writes are idempotent.
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("build memory digests: %w", err)
	}

	result.Built = int(built.Load())
	result.Skipped = int(skipped.Load())
	result.Failed = int(failed.Load())
	return result, nil
}

// invalidate deletes digests whose sources changed after they were written,
// so the job rebuilds them: day digests that folded a chat or memo no longer
// eligible, then week digests that folded a deleted day, then month digests
// that folded a deleted week. One pass removes the whole chain. A period
// older than the lookback is deleted but not rebuilt; its sources then fall
// back to search.
func (m *MemoryDigests) invalidate(ctx context.Context) error {
	ids, err := m.repo.DeleteStaleDayDigests(ctx)
	if err != nil {
		return fmt.Errorf("invalidate day digests: %w", err)
	}
	deleted := len(ids)
	for _, level := range []memorydigest.Level{memorydigest.LevelWeek, memorydigest.LevelMonth} {
		ids, err := m.repo.DeleteOrphanedRollups(ctx, string(level))
		if err != nil {
			return fmt.Errorf("invalidate %s digests: %w", level, err)
		}
		deleted += len(ids)
	}
	if deleted > 0 {
		m.logger.InfoContext(ctx, "invalidated stale memory digests", attr.SlogDBDeletedRowsCount(int64(deleted)))
	}
	return nil
}

func (m *MemoryDigests) listCandidates(ctx context.Context, level memorydigest.Level, now time.Time, orgIDs []string) ([]memoryDigestCandidate, error) {
	since := level.PeriodStart(now.Add(-MemoryDigestLookback))
	// One extra row tells the caller another batch is waiting.
	limit := int32(memoryDigestBatchSize + 1)

	var out []memoryDigestCandidate
	if level == memorydigest.LevelDay {
		rows, err := m.repo.ListDayDigestCandidates(ctx, repo.ListDayDigestCandidatesParams{
			OrganizationIds: orgIDs,
			Since:           pgtype.Timestamptz{Time: since, Valid: true, InfinityModifier: pgtype.Finite},
			Until:           pgtype.Timestamptz{Time: level.PeriodStart(now), Valid: true, InfinityModifier: pgtype.Finite},
			RowLimit:        limit,
		})
		if err != nil {
			return nil, fmt.Errorf("list day digest candidates: %w", err)
		}
		for _, r := range rows {
			out = append(out, memoryDigestCandidate{organizationID: r.OrganizationID, projectID: r.ProjectID, periodStart: r.PeriodStart.Time})
		}
		return out, nil
	}

	child, _ := level.ChildLevel()
	rows, err := m.repo.ListRollupDigestCandidates(ctx, repo.ListRollupDigestCandidatesParams{
		ParentLevel:     string(level),
		ChildLevel:      string(child),
		OrganizationIds: orgIDs,
		Since:           pgDate(since),
		// Children of the current, open period are excluded here; a month
		// can still be open after its own start has passed, which the
		// ReadyAt filter below catches.
		Until:    pgDate(level.PeriodStart(now)),
		RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list %s digest candidates: %w", level, err)
	}
	for _, r := range rows {
		start := r.PeriodStart.Time
		if level.ReadyAt(start).After(now) {
			continue
		}
		out = append(out, memoryDigestCandidate{organizationID: r.OrganizationID, projectID: r.ProjectID, periodStart: start})
	}
	return out, nil
}

// enabledOrganizations lists the organizations with team memory activity in
// the lookback whose rollout flag is on. It fails closed: an org whose flag
// cannot be evaluated is left out of this run and retried on the next.
func (m *MemoryDigests) enabledOrganizations(ctx context.Context, now time.Time) ([]string, error) {
	orgIDs, err := m.repo.ListMemoryOrganizations(ctx, pgtype.Timestamptz{Time: now.Add(-MemoryDigestLookback), Valid: true, InfinityModifier: pgtype.Finite})
	if err != nil {
		return nil, fmt.Errorf("list team memory organizations: %w", err)
	}

	enabled := make([]string, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		org, err := orgrepo.New(m.db).GetOrganizationMetadata(ctx, orgID)
		if err != nil {
			m.logger.WarnContext(ctx, "resolve organization slug for team memory flag", attr.SlogError(err), attr.SlogOrganizationID(orgID))
			continue
		}
		on, err := m.flags.IsFlagEnabled(ctx, feature.FlagTeamMemory, orgID, feature.OrgProjectGroups(org.Slug, ""))
		if err != nil {
			m.logger.WarnContext(ctx, "evaluate team memory flag", attr.SlogError(err), attr.SlogOrganizationID(orgID))
			continue
		}
		if on {
			enabled = append(enabled, orgID)
		}
	}
	return enabled, nil
}

// buildOne writes the digest for one project period. It reports false
// without an error when there was nothing to fold or the digest already
// exists.
func (m *MemoryDigests) buildOne(ctx context.Context, level memorydigest.Level, c memoryDigestCandidate) (bool, error) {
	start := level.PeriodStart(c.periodStart)
	end := level.PeriodEnd(start)

	params := repo.InsertDigestParams{
		OrganizationID: c.organizationID,
		ProjectID:      c.projectID,
		Level:          string(level),
		PeriodStart:    pgDate(start),
		PeriodEnd:      pgDate(end),
		Content:        "",
		ChatIds:        []uuid.UUID{},
		MemoIds:        []uuid.UUID{},
		ChildDigestIds: []uuid.UUID{},
		Model:          conv.ToPGText(m.model),
		InputTokens:    pgtype.Int4{Int32: 0, Valid: false},
		OutputTokens:   pgtype.Int4{Int32: 0, Valid: false},
	}

	var inputs []memorydigest.Input
	if child, isRollup := level.ChildLevel(); isRollup {
		children, err := m.repo.ListChildDigests(ctx, repo.ListChildDigestsParams{
			ProjectID:      c.projectID,
			OrganizationID: c.organizationID,
			Level:          string(child),
			Since:          pgDate(start),
			Until:          pgDate(end),
		})
		if err != nil {
			return false, fmt.Errorf("list child digests: %w", err)
		}
		for _, d := range children {
			inputs = append(inputs, memorydigest.Input{
				Kind:  memorydigest.SourceDigest,
				ID:    d.ID,
				At:    d.PeriodStart.Time,
				Title: child.Label(d.PeriodStart.Time),
				Text:  d.Content,
			})
		}
	} else {
		var err error
		inputs, err = m.dayInputs(ctx, c, start, end)
		if err != nil {
			return false, err
		}
	}

	system, user, used := level.Prompt(start, inputs)
	if len(used) == 0 {
		return false, nil
	}
	// Record only what the prompt held. Sources left out by the caps stay
	// uncovered, so recall still shows them on their own.
	for _, in := range used {
		switch in.Kind {
		case memorydigest.SourceChat:
			params.ChatIds = append(params.ChatIds, in.ID)
		case memorydigest.SourceMemo:
			params.MemoIds = append(params.MemoIds, in.ID)
		case memorydigest.SourceDigest:
			params.ChildDigestIds = append(params.ChildDigestIds, in.ID)
		}
	}

	content, usage, err := m.complete(ctx, c, system, user)
	if err != nil {
		return false, err
	}
	params.Content = content
	params.InputTokens = pgtype.Int4{Int32: conv.SafeInt32(usage.PromptTokens), Valid: true}
	params.OutputTokens = pgtype.Int4{Int32: conv.SafeInt32(usage.CompletionTokens), Valid: true}

	if _, err := m.repo.InsertDigest(ctx, params); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // Another run wrote this period first.
		}
		return false, fmt.Errorf("insert digest: %w", err)
	}
	return true, nil
}

// dayInputs loads a day's eligible chat summaries and memos in
// chronological order.
func (m *MemoryDigests) dayInputs(ctx context.Context, c memoryDigestCandidate, start, end time.Time) ([]memorydigest.Input, error) {
	chats, err := m.repo.ListDayDigestChats(ctx, repo.ListDayDigestChatsParams{
		ProjectID:      c.projectID,
		OrganizationID: c.organizationID,
		PeriodStart:    pgtype.Timestamptz{Time: start, Valid: true, InfinityModifier: pgtype.Finite},
		PeriodEnd:      pgtype.Timestamptz{Time: end, Valid: true, InfinityModifier: pgtype.Finite},
		RowLimit:       maxDayDigestChats,
	})
	if err != nil {
		return nil, fmt.Errorf("list day digest chats: %w", err)
	}
	memos, err := m.repo.ListDayDigestMemos(ctx, repo.ListDayDigestMemosParams{
		ProjectID:      c.projectID,
		OrganizationID: c.organizationID,
		PeriodStart:    pgtype.Timestamptz{Time: start, Valid: true, InfinityModifier: pgtype.Finite},
		PeriodEnd:      pgtype.Timestamptz{Time: end, Valid: true, InfinityModifier: pgtype.Finite},
		RowLimit:       maxDayDigestMemos,
	})
	if err != nil {
		return nil, fmt.Errorf("list day digest memos: %w", err)
	}

	inputs := make([]memorydigest.Input, 0, len(chats)+len(memos))
	for _, ch := range chats {
		inputs = append(inputs, memorydigest.Input{
			Kind: memorydigest.SourceChat,
			ID:   ch.ID,
			At:   ch.SummaryGeneratedAt.Time,
			// Titles are generated from the raw transcript and are not
			// masked at rest, so only the masked summary reaches the model.
			Title: "",
			Text:  ch.Summary,
		})
	}
	for _, memo := range memos {
		inputs = append(inputs, memorydigest.Input{
			Kind:  memorydigest.SourceMemo,
			ID:    memo.ID,
			At:    memo.CreatedAt.Time,
			Title: "",
			Text:  memo.Content,
		})
	}
	slices.SortStableFunc(inputs, func(a, b memorydigest.Input) int { return a.At.Compare(b.At) })
	return inputs, nil
}

func (m *MemoryDigests) complete(ctx context.Context, c memoryDigestCandidate, system, user string) (string, openrouter.Usage, error) {
	if m.chatClient == nil {
		return "", openrouter.Usage{}, errors.New("no completion client configured")
	}

	ctx, cancel := context.WithTimeout(ctx, memoryDigestCompletionTimeout)
	defer cancel()

	response, err := m.chatClient.GetCompletion(ctx, openrouter.CompletionRequest{
		OrgID:     c.organizationID,
		ProjectID: c.projectID.String(),
		ChatID:    uuid.Nil,
		Messages: []or.ChatMessages{
			openrouter.CreateMessageSystem(system),
			openrouter.CreateMessageUser(user),
		},
		MaxTokens:                 new(memoryDigestMaxOutputTokens),
		Tools:                     nil,
		ToolChoice:                nil,
		Temperature:               nil,
		Model:                     m.model,
		Stream:                    false,
		UsageSource:               billing.ModelUsageSourceGram,
		KeyType:                   openrouter.KeyTypeInternal,
		KeySlot:                   "",
		UserID:                    "",
		ExternalUserID:            "",
		UserEmail:                 "",
		HTTPMetadata:              nil,
		APIKeyID:                  "",
		JSONSchema:                nil,
		Reasoning:                 nil,
		CacheControl:              nil,
		NormalizeOutboundMessages: false,
		WebSearch:                 nil,
		DisableResponseHealing:    false,
	})
	if err != nil {
		return "", openrouter.Usage{}, fmt.Errorf("digest completion: %w", err)
	}
	if response == nil || response.Message == nil {
		return "", openrouter.Usage{}, errors.New("digest completion returned no message")
	}
	content := strings.TrimSpace(openrouter.GetText(*response.Message))
	if content == "" {
		return "", openrouter.Usage{}, errors.New("digest completion returned empty text")
	}
	return content, response.Usage, nil
}

func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true, InfinityModifier: pgtype.Finite}
}

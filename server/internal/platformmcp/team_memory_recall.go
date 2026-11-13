package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/memorydigest"
	memorydigestrepo "github.com/speakeasy-api/gram/server/internal/memorydigest/repo"
)

const (
	recallTeamHistoryToolName = "recall_team_history"
	// teamMemoryRecallLookback is how far back the recall block reaches.
	// Older history is still reachable through search_team_memory.
	teamMemoryRecallLookback = 366 * 24 * time.Hour
	// teamMemoryRecallDefaultTokens sizes the block when the caller does not.
	// It leaves room for a follow-up zoom or search inside the eval's tool
	// token gate (twice what search_chats spends per question).
	teamMemoryRecallDefaultTokens = 4_000
	teamMemoryRecallMinTokens     = 1_000
	teamMemoryRecallMaxTokens     = memorydigest.DefaultRecallBudgetTokens
	// teamMemoryRecallRowLimit caps the chats and memos not yet folded into a
	// digest, newest first. At most a day or two of activity is unfolded
	// when the nightly job runs, so the cap only bites on a busy project
	// whose digests are behind.
	teamMemoryRecallRowLimit = 200
)

// RecallTeamHistoryInput asks for one project's history as one block.
type RecallTeamHistoryInput struct {
	ProjectID string `json:"project_id,omitempty"`

	ProjectSlug string `json:"project_slug,omitempty"`

	// MaxTokens bounds the block's estimated size. Defaults to 4,000.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// RecallTeamHistoryOutput is the rendered history and what it cites.
type RecallTeamHistoryOutput struct {
	// History is the block, oldest first. Each section is headed by its
	// period or date and a citation such as [digest:<id>].
	History string `json:"history"`

	// Sources lists every item the block shows, oldest first.
	Sources []TeamMemorySource `json:"sources"`

	// Omitted counts older sections left out to fit MaxTokens.
	Omitted int `json:"omitted"`

	// Since is the earliest time the block reaches back to.
	Since time.Time `json:"since"`

	// Capped is true when more summaries or memos are waiting for a digest
	// than the block reads; the oldest of them are left out and are reachable
	// through search_team_memory.
	Capped bool `json:"capped"`
}

// teamMemoryRecallCappedNote ends the block when Capped is true.
const teamMemoryRecallCappedNote = "\n\n(More recent sessions and memos are waiting for a digest than fit here; the oldest of them are left out. Use search_team_memory to reach them.)"

// Recall renders a project's last year of team memory as one block: month
// digests for older history, then the week and day digests no month or week
// digest folded yet, then summaries and memos no day digest folded yet. The
// newest sections are kept when the block outgrows its budget. Stale digests
// (ones that folded something no longer eligible) are left out by the
// queries, and their still-eligible sources come back on their own.
// Summaries and memos are masked again before they are cut to a snippet, so
// a cut never splits a secret the masker would otherwise catch.
func (s *TeamMemoryService) Recall(ctx context.Context, principal Principal, input RecallTeamHistoryInput) (RecallTeamHistoryOutput, error) {
	var zero RecallTeamHistoryOutput
	budget := input.MaxTokens
	if budget == 0 {
		budget = teamMemoryRecallDefaultTokens
	}
	if budget < teamMemoryRecallMinTokens || budget > teamMemoryRecallMaxTokens {
		return zero, errTeamMemoryInvalid
	}
	project, err := s.authorizeRead(ctx, principal, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, teamMemorySearchTimeout)
	defer cancel()
	output, err := s.recall(ctx, principal, project, budget, s.clock())
	if errors.Is(err, context.DeadlineExceeded) {
		return zero, errTeamMemoryTimeout
	}
	return output, err
}

func (s *TeamMemoryService) recall(ctx context.Context, principal Principal, project ResolvedProject, budget int, now time.Time) (RecallTeamHistoryOutput, error) {
	var zero RecallTeamHistoryOutput
	since := now.Add(-teamMemoryRecallLookback)
	queries := memorydigestrepo.New(s.db)
	digestRows, err := queries.ListRecallDigests(ctx, memorydigestrepo.ListRecallDigestsParams{
		ProjectID: project.ID, OrganizationID: principal.OrganizationID, Since: pgtype.Date{Time: since.Truncate(24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	if err != nil {
		return zero, fmt.Errorf("list recall digests: %w", err)
	}
	chatRows, err := queries.ListRecallChats(ctx, memorydigestrepo.ListRecallChatsParams{
		ProjectID: project.ID, OrganizationID: principal.OrganizationID, Since: conv.ToPGTimestamptz(since), RowLimit: teamMemoryRecallRowLimit + 1,
	})
	if err != nil {
		return zero, fmt.Errorf("list recall chats: %w", err)
	}
	memoRows, err := queries.ListRecallMemos(ctx, memorydigestrepo.ListRecallMemosParams{
		ProjectID: project.ID, OrganizationID: principal.OrganizationID, Since: conv.ToPGTimestamptz(since), RowLimit: teamMemoryRecallRowLimit + 1,
	})
	if err != nil {
		return zero, fmt.Errorf("list recall memos: %w", err)
	}
	// One row past the cap tells a full page from a cut one.
	capped := len(chatRows) > teamMemoryRecallRowLimit || len(memoRows) > teamMemoryRecallRowLimit
	chatRows = chatRows[:min(len(chatRows), teamMemoryRecallRowLimit)]
	memoRows = memoRows[:min(len(memoRows), teamMemoryRecallRowLimit)]

	digests := make([]memorydigest.Digest, 0, len(digestRows))
	for _, row := range digestRows {
		level, err := memorydigest.ParseLevel(row.Level)
		if err != nil {
			return zero, fmt.Errorf("recall digest level: %w", err)
		}
		digests = append(digests, memorydigest.Digest{
			ID: row.ID, Level: level, PeriodStart: row.PeriodStart.Time.UTC(), Content: row.Content,
			ChildDigestIDs: row.ChildDigestIds, ChatIDs: row.ChatIds, MemoIDs: row.MemoIds,
		})
	}
	chats := make([]memorydigest.ChatNote, 0, len(chatRows))
	for _, row := range chatRows {
		summary, _, err := s.redactor.Text(ctx, row.Summary)
		if err != nil {
			return zero, fmt.Errorf("redact recall chat: %w", err)
		}
		chats = append(chats, memorydigest.ChatNote{ID: row.ID, SummarizedAt: row.SummaryGeneratedAt.Time.UTC(), Summary: summary})
	}
	memos := make([]memorydigest.MemoNote, 0, len(memoRows))
	memoChats := make(map[uuid.UUID]string, len(memoRows))
	for _, row := range memoRows {
		if row.ChatID.Valid {
			memoChats[row.ID] = row.ChatID.UUID.String()
		}
		content, _, err := s.redactor.Text(ctx, row.Content)
		if err != nil {
			return zero, fmt.Errorf("redact recall memo: %w", err)
		}
		memos = append(memos, memorydigest.MemoNote{ID: row.ID, CreatedAt: row.CreatedAt.Time.UTC(), Content: content})
	}

	// Digest sections carry the whole digest, so they are masked after
	// selection: only the uncovered ones are shown.
	entries := memorydigest.Uncovered(digests, chats, memos)
	for i, entry := range entries {
		if entry.Kind != memorydigest.SourceDigest {
			continue
		}
		text, _, err := s.redactor.Text(ctx, entry.Text)
		if err != nil {
			return zero, fmt.Errorf("redact recall digest: %w", err)
		}
		entries[i].Text = text
	}
	kept, omitted := memorydigest.Budget(entries, budget)

	output := RecallTeamHistoryOutput{History: memorydigest.Render(kept, omitted), Sources: make([]TeamMemorySource, 0, len(kept)), Omitted: omitted, Since: since, Capped: capped}
	if capped {
		output.History += teamMemoryRecallCappedNote
	}
	for _, entry := range kept {
		source := TeamMemorySource{Kind: string(entry.Kind), ID: entry.ID.String(), ChatID: ""}
		if entry.Kind == memorydigest.SourceMemo {
			source.ChatID = memoChats[entry.ID]
		}
		output.Sources = append(output.Sources, source)
	}
	if err := s.recordRead(ctx, principal, project, recallTeamHistoryToolName, output.Sources); err != nil {
		return zero, err
	}
	return output, nil
}

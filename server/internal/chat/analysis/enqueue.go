package analysis

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
)

// EnqueuePageResult reports what one page did for a project.
type EnqueuePageResult struct {
	// Scanned is the number of candidate chats the page read. It never exceeds
	// the page size.
	Scanned int
	// NextCursor is where the next page starts. It is strictly past the cursor
	// the page was given whenever the page read anything, and equal to it when
	// the queue was already empty. Chaining it is what carries a walk across
	// calls.
	NextCursor EnqueueCursor
	// Exhausted reports that the page reached the end of the candidate set — it
	// read fewer chats than it asked for. A caller resumes from NextCursor when
	// it is false and stops when it is true.
	Exhausted bool
}

// EnqueuePage turns one bounded page of a project's chats into pending
// evaluations, one per (chat, judge) unit for every judge the organization has
// enabled.
//
// This is the durable primitive the pipeline is built on: a call reads at most
// pageSize chats in one short pass and returns the cursor it stopped at, so a
// coordinator — a Temporal workflow persisting NextCursor between activities —
// decides how far a walk goes. Pass a zero EnqueueCursor to start at the head.
// The walk is bounded to EnqueueLookback, and the cursor key (created_at, id)
// is immutable, so a restarted walk covers exactly the chats a continuous one
// would have.
//
// Quiet is not checked here: the insert stores the chat's latest message time
// as observed_at, and the reservation's candidate read applies the inactivity
// window live. An organization with no enabled judge gets no queue built for it
// at all, rather than a backlog of pending rows nothing will ever reserve.
func EnqueuePage(ctx context.Context, db *pgxpool.Pool, judges *Judges, projectID uuid.UUID, cursor EnqueueCursor, pageSize int32) (EnqueuePageResult, error) {
	if pageSize <= 0 || pageSize > MaxEnqueuePageSize {
		return EnqueuePageResult{}, fmt.Errorf("enqueue chat analysis evaluations: page size must be between 1 and %d, got %d", MaxEnqueuePageSize, pageSize)
	}

	queries := repo.New(db)

	settings, err := settingsForProject(ctx, queries, judges, projectID)
	if err != nil {
		return EnqueuePageResult{}, err
	}
	enabled := settings.enabledJudges()
	if len(enabled) == 0 {
		return EnqueuePageResult{Scanned: 0, NextCursor: EnqueueCursor{CreatedAt: time.Time{}, ID: uuid.Nil}, Exhausted: true}, nil
	}
	// Deterministic row order inside the insert's unnest expansion.
	slices.Sort(enabled)

	// A chat id is never nil, so it is what distinguishes a resumed cursor from
	// the zero value that starts at the head.
	started := cursor.ID != uuid.Nil
	page, err := queries.ListChatAnalysisCandidateChats(ctx, repo.ListChatAnalysisCandidateChatsParams{
		ProjectID:       projectID,
		Lookback:        pgtype.Interval{Microseconds: EnqueueLookback.Microseconds(), Days: 0, Months: 0, Valid: true},
		CursorCreatedAt: pgtype.Timestamptz{Time: cursor.CreatedAt, InfinityModifier: pgtype.Finite, Valid: started},
		CursorID:        uuid.NullUUID{UUID: cursor.ID, Valid: started},
		PageSize:        pageSize,
	})
	if err != nil {
		return EnqueuePageResult{}, fmt.Errorf("list chat analysis candidate chats: %w", err)
	}
	if len(page) == 0 {
		return EnqueuePageResult{Scanned: 0, NextCursor: cursor, Exhausted: true}, nil
	}

	// organization_id is not carried here: the insert derives it from the
	// project row so the queue can never disagree with the projects table the
	// spend count joins through.
	insert := repo.EnqueueChatAnalysisEvaluationsParams{
		ProjectID: projectID,
		ChatIds:   make([]uuid.UUID, 0, len(page)),
		// The chats walk has no raw session id to carry: the chat IS the session
		// here, and judges that need the original session string bring their own
		// unit source.
		SessionIds:  make([]string, 0, len(page)),
		ObservedAts: make([]pgtype.Timestamptz, 0, len(page)),
		Judges:      enabled,
	}
	for _, candidate := range page {
		insert.ChatIds = append(insert.ChatIds, candidate.ID)
		insert.SessionIds = append(insert.SessionIds, "")
		insert.ObservedAts = append(insert.ObservedAts, candidate.LastMessageAt)
	}
	if err := queries.EnqueueChatAnalysisEvaluations(ctx, insert); err != nil {
		return EnqueuePageResult{}, fmt.Errorf("insert chat analysis evaluations: %w", err)
	}

	last := page[len(page)-1]

	return EnqueuePageResult{
		Scanned:    len(page),
		NextCursor: EnqueueCursor{CreatedAt: last.CreatedAt.Time, ID: last.ID},
		Exhausted:  len(page) < int(pageSize),
	}, nil
}

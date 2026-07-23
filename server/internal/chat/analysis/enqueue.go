package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
)

// EnqueuePageResult reports what one page of one enqueue source did for a
// project. The cursor is opaque to the caller: the chats walk and each custom
// unit source own their own encoding, and a coordinator simply hands back what
// it was given last.
type EnqueuePageResult struct {
	// Scanned is the number of source rows the page read. It never exceeds the
	// page size.
	Scanned int `json:"scanned"`
	// NextCursor is where the next page starts. Chaining it is what carries a
	// walk across calls.
	NextCursor json.RawMessage `json:"next_cursor"`
	// Exhausted reports that the page reached the end of the source. A caller
	// resumes from NextCursor when it is false and stops when it is true.
	Exhausted bool `json:"exhausted"`
}

// EnqueuePage runs one bounded page of one enqueue source for a project.
//
// The ChatsEnqueueSource walks the project's chats and enqueues one (chat,
// judge) unit per enabled judge that has no unit source of its own. Every other
// source names a judge implementing UnitSource, which derives units from its
// domain data. Either way a page is one short pass, and a coordinator — a
// Temporal workflow persisting NextCursor between activities — decides how far
// a walk goes. Pass a nil cursor to start at the head.
//
// A source whose judge the organization has not enabled enqueues nothing and
// reports itself exhausted, so no queue is ever built for work no reservation
// can spend on.
func EnqueuePage(ctx context.Context, db *pgxpool.Pool, judges *Judges, projectID uuid.UUID, source string, cursor json.RawMessage, pageSize int32) (EnqueuePageResult, error) {
	if pageSize <= 0 || pageSize > MaxEnqueuePageSize {
		return EnqueuePageResult{}, fmt.Errorf("enqueue chat analysis evaluations: page size must be between 1 and %d, got %d", MaxEnqueuePageSize, pageSize)
	}

	settings, err := settingsForProject(ctx, repo.New(db), judges, projectID)
	if err != nil {
		return EnqueuePageResult{}, err
	}

	if source == ChatsEnqueueSource {
		return enqueueChatsPage(ctx, db, judges, settings, projectID, cursor, pageSize)
	}

	judge, ok := judges.Get(source)
	if !ok {
		return EnqueuePageResult{}, fmt.Errorf("enqueue chat analysis evaluations: unknown source %q", source)
	}
	unitSource, ok := judge.(UnitSource)
	if !ok {
		return EnqueuePageResult{}, fmt.Errorf("enqueue chat analysis evaluations: judge %q has no unit source", source)
	}
	if settings.JudgeDailyCaps[source] <= 0 {
		return EnqueuePageResult{Scanned: 0, NextCursor: nil, Exhausted: true}, nil
	}

	page, err := unitSource.EnqueueUnitsPage(ctx, db, projectID, cursor, pageSize)
	if err != nil {
		return EnqueuePageResult{}, fmt.Errorf("enqueue %s units page: %w", source, err)
	}

	return EnqueuePageResult(page), nil
}

// enqueueChatsPage turns one bounded page of a project's chats into pending
// evaluations, one per (chat, judge) unit for every enabled judge the shared
// walk covers.
//
// The walk is bounded to EnqueueLookback, and the cursor key (created_at, id)
// is immutable, so a restarted walk covers exactly the chats a continuous one
// would have. Quiet is not checked here: the insert stores the chat's latest
// message time as observed_at, and the reservation's candidate read applies the
// inactivity window live.
func enqueueChatsPage(ctx context.Context, db *pgxpool.Pool, judges *Judges, settings Settings, projectID uuid.UUID, rawCursor json.RawMessage, pageSize int32) (EnqueuePageResult, error) {
	walkJudges := judges.chatsWalkJudges()
	enabled := make([]string, 0, len(walkJudges))
	for _, name := range settings.enabledJudges() {
		if _, ok := walkJudges[name]; ok {
			enabled = append(enabled, name)
		}
	}
	if len(enabled) == 0 {
		return EnqueuePageResult{Scanned: 0, NextCursor: nil, Exhausted: true}, nil
	}
	// Deterministic row order inside the insert's unnest expansion.
	slices.Sort(enabled)

	var cursor EnqueueCursor
	if len(rawCursor) > 0 {
		if err := json.Unmarshal(rawCursor, &cursor); err != nil {
			return EnqueuePageResult{}, fmt.Errorf("decode chats enqueue cursor: %w", err)
		}
	}

	queries := repo.New(db)

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
		return EnqueuePageResult{Scanned: 0, NextCursor: rawCursor, Exhausted: true}, nil
	}

	insert := repo.EnqueueChatAnalysisEvaluationsParams{
		ProjectID:       projectID,
		OrganizationIds: make([]string, 0, len(page)),
		ChatIds:         make([]uuid.UUID, 0, len(page)),
		// The chats walk has no raw session id to carry: the chat IS the session
		// here, and judges that need the original session string bring their own
		// unit source.
		SessionIds:  make([]string, 0, len(page)),
		ObservedAts: make([]pgtype.Timestamptz, 0, len(page)),
		Judges:      enabled,
	}
	for _, candidate := range page {
		insert.OrganizationIds = append(insert.OrganizationIds, candidate.OrganizationID)
		insert.ChatIds = append(insert.ChatIds, candidate.ID)
		insert.SessionIds = append(insert.SessionIds, "")
		insert.ObservedAts = append(insert.ObservedAts, candidate.LastMessageAt)
	}
	if err := queries.EnqueueChatAnalysisEvaluations(ctx, insert); err != nil {
		return EnqueuePageResult{}, fmt.Errorf("insert chat analysis evaluations: %w", err)
	}

	last := page[len(page)-1]
	next, err := json.Marshal(EnqueueCursor{CreatedAt: last.CreatedAt.Time, ID: last.ID})
	if err != nil {
		return EnqueuePageResult{}, fmt.Errorf("encode chats enqueue cursor: %w", err)
	}

	return EnqueuePageResult{
		Scanned:    len(page),
		NextCursor: next,
		Exhausted:  len(page) < int(pageSize),
	}, nil
}

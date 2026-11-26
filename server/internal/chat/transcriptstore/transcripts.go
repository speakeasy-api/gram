package transcriptstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

// Transcripts is the one entry point for the bodies of chat messages. It owns
// the decision a caller would otherwise repeat: whether a project has encrypted
// transcripts, so its messages are sealed on write and opened on read. Today
// that is the projects on GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS. Every other
// project reads plaintext: Seal and Open do nothing for it. A nil *Transcripts
// means no project has encrypted transcripts.
type Transcripts struct {
	store    *Store
	projects map[uuid.UUID]struct{}

	// organizations caches each listed project's organization, which opening
	// a shadow needs and most readers do not have at hand.
	organizations *sync.Map

	// guard reports a read for a listed project that this process cannot
	// open, because it has no store. See plaintext_guard.go.
	guard *plaintextGuard
}

// NewTranscripts encrypts transcripts for the listed projects only. An empty list
// turns every method into a no-op; there is no implicit all-project mode.
func NewTranscripts(store *Store, projectIDs ...uuid.UUID) *Transcripts {
	projects := make(map[uuid.UUID]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		projects[projectID] = struct{}{}
	}
	return &Transcripts{store: store, projects: projects, organizations: &sync.Map{}, guard: nil}
}

// Enabled reports whether the project has encrypted transcripts.
func (t *Transcripts) Enabled(projectID uuid.UUID) bool {
	if t == nil || t.store == nil {
		return false
	}
	_, ok := t.projects[projectID]
	return ok
}

// Seal stores the encrypted copy of messages the caller just wrote, inside the
// caller's transaction. The caller must roll back on error.
func (t *Transcripts) Seal(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) error {
	if !t.Enabled(projectID) {
		return nil
	}
	if err := t.store.Capture(ctx, db, organizationID, projectID, messageIDs); err != nil {
		return fmt.Errorf("seal chat transcript: %w", err)
	}
	return nil
}

// Body points at the body fields of one loaded message, so Open can replace
// them with the opened copy whatever query loaded the row. A nil field was
// not loaded and is left alone.
type Body struct {
	MessageID         uuid.UUID
	Content           *string
	ContentRaw        *[]byte
	ToolCalls         *[]byte
	ToolOutcomeNotes  *pgtype.Text
	ToolCallSummaries *[]byte
	StorageError      *pgtype.Text
}

// Open replaces each body with its encrypted copy when the project has
// encrypted transcripts, in one LoadMany call. A message with no encrypted copy
// yet keeps its plaintext and counts as plaintext_fallback, so messages written
// before sealing began still load. A stale or unopenable copy fails the whole
// read: it means sealing or the data is broken, and plaintext must not hide
// that.
func (t *Transcripts) Open(ctx context.Context, organizationID string, projectID uuid.UUID, bodies []Body) error {
	if len(bodies) == 0 {
		return nil
	}
	if err := t.checkOpenable(ctx, projectID, "Open"); err != nil {
		return err
	}
	if !t.Enabled(projectID) {
		return nil
	}
	ids := make([]uuid.UUID, len(bodies))
	for i, b := range bodies {
		ids[i] = b.MessageID
	}
	opened, err := t.store.LoadMany(ctx, organizationID, projectID, ids)
	if err != nil {
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	for _, b := range bodies {
		message, ok := opened[b.MessageID]
		if !ok {
			continue
		}
		b.assign(message)
	}
	return nil
}

// OpenRows is Open for a page of rows of any sqlc row type. body maps a row to
// pointers at its body fields.
func OpenRows[T any](ctx context.Context, t *Transcripts, organizationID string, projectID uuid.UUID, rows []T, body func(*T) Body) error {
	if len(rows) == 0 {
		return nil
	}
	if !t.Enabled(projectID) {
		// Open reports a listed project this process cannot open.
		return t.checkOpenable(ctx, projectID, "OpenRows")
	}
	bodies := make([]Body, len(rows))
	for i := range rows {
		bodies[i] = body(&rows[i])
	}
	return t.Open(ctx, organizationID, projectID, bodies)
}

func optionalText(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: *v, Valid: true}
}

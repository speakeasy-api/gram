package transcriptstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

// Transcripts is the one entry point for the bodies of chat messages in
// projects that keep encrypted shadows. It owns the decision a caller would
// otherwise repeat: whether a project captures shadows on write and opens them
// on read. Every other project keeps plaintext: Capture and Open do nothing for
// it. A nil *Transcripts means no project keeps shadows.
type Transcripts struct {
	store    *Store
	projects map[uuid.UUID]struct{}
}

// NewTranscripts serves shadows for the listed projects only. An empty list
// turns every method into a no-op; there is no implicit all-project mode.
func NewTranscripts(store *Store, projectIDs ...uuid.UUID) *Transcripts {
	projects := make(map[uuid.UUID]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		projects[projectID] = struct{}{}
	}
	return &Transcripts{store: store, projects: projects}
}

// Enabled reports whether the project captures and reads shadows.
func (t *Transcripts) Enabled(projectID uuid.UUID) bool {
	if t == nil || t.store == nil {
		return false
	}
	_, ok := t.projects[projectID]
	return ok
}

// Capture seals shadows for messages the caller just wrote, inside the
// caller's transaction. The caller must roll back on error.
func (t *Transcripts) Capture(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) error {
	if !t.Enabled(projectID) {
		return nil
	}
	if err := t.store.Capture(ctx, db, organizationID, projectID, messageIDs); err != nil {
		return fmt.Errorf("capture encrypted transcript shadow: %w", err)
	}
	return nil
}

// Body points at the body fields of one loaded message, so Open can replace
// them with the opened shadow whatever query loaded the row. A nil field was
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

// Open replaces each body with its encrypted shadow when the project keeps
// shadows, in one LoadMany call. A message with no shadow at all keeps its
// plaintext and counts as plaintext_fallback, so messages written before
// capture began still load. A stale or unopenable shadow fails the whole read:
// it means capture or the data is broken, and plaintext must not hide that.
func (t *Transcripts) Open(ctx context.Context, organizationID string, projectID uuid.UUID, bodies []Body) error {
	if len(bodies) == 0 || !t.Enabled(projectID) {
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
		if b.Content != nil {
			*b.Content = message.Content
		}
		if b.ContentRaw != nil {
			*b.ContentRaw = message.ContentRaw
		}
		if b.ToolCalls != nil {
			*b.ToolCalls = message.ToolCalls
		}
		if b.ToolCallSummaries != nil {
			*b.ToolCallSummaries = message.ToolCallSummaries
		}
		if b.ToolOutcomeNotes != nil {
			*b.ToolOutcomeNotes = optionalText(message.ToolOutcomeNotes)
		}
		if b.StorageError != nil {
			*b.StorageError = optionalText(message.StorageError)
		}
	}
	return nil
}

// OpenRows is Open for a page of rows of any sqlc row type. body maps a row to
// pointers at its body fields.
func OpenRows[T any](ctx context.Context, t *Transcripts, organizationID string, projectID uuid.UUID, rows []T, body func(*T) Body) error {
	if len(rows) == 0 || !t.Enabled(projectID) {
		return nil
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

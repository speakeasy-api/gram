package transcriptstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
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
}

// NewTranscripts encrypts transcripts for the listed projects only. An empty list
// turns every method into a no-op; there is no implicit all-project mode. Listed
// projects without a store are a wiring mistake that would silently skip
// encryption, so it panics.
func NewTranscripts(store *Store, projectIDs ...uuid.UUID) *Transcripts {
	if store == nil && len(projectIDs) > 0 {
		panic("transcriptstore: NewTranscripts needs a store when projects are listed")
	}
	projects := make(map[uuid.UUID]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		projects[projectID] = struct{}{}
	}
	return &Transcripts{store: store, projects: projects, organizations: &sync.Map{}}
}

// Enabled reports whether the project has encrypted transcripts.
func (t *Transcripts) Enabled(projectID uuid.UUID) bool {
	if t == nil || t.store == nil {
		return false
	}
	_, ok := t.projects[projectID]
	return ok
}

// Prepare loads the organization's transcript key into the process cache
// before the caller opens the transaction that Seal will use, so Seal makes no
// KMS call while the transaction is open. An error means Seal would fail too.
func (t *Transcripts) Prepare(ctx context.Context, organizationID string, projectID uuid.UUID) error {
	if !t.Enabled(projectID) {
		return nil
	}
	if err := t.store.Prepare(ctx, organizationID); err != nil {
		return fmt.Errorf("prepare encrypted chat transcript: %w", err)
	}
	return nil
}

// Seal stores the encrypted copy of messages the caller just wrote, inside the
// caller's transaction. Call Prepare before beginning that transaction. The
// caller must roll back on error.
func (t *Transcripts) Seal(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) error {
	if !t.Enabled(projectID) {
		return nil
	}
	if err := t.store.Capture(ctx, db, organizationID, projectID, messageIDs); err != nil {
		return fmt.Errorf("seal chat transcript: %w", err)
	}
	return nil
}

// Mode is how a project stores message bodies.
type Mode int

const (
	// ModePlaintext keeps bodies in the plaintext columns only.
	ModePlaintext Mode = iota
	// ModeShadow keeps the plaintext columns and an encrypted transcript of
	// the same body. Reads open the transcript; writes keep both current.
	ModeShadow
	// ModeEncrypted keeps the body only in the encrypted transcript. It is not
	// available yet: no project is put in it.
	ModeEncrypted
)

// Mode reports how the project stores message bodies.
func (t *Transcripts) Mode(projectID uuid.UUID) Mode {
	if t.Enabled(projectID) {
		return ModeShadow
	}
	return ModePlaintext
}

// UpdateBody is the one way to change the body of an existing message: content,
// content_raw, tool_calls, tool_outcome_notes, tool_call_summaries and
// storage_error. Inside the caller's transaction it locks the message row,
// reads the body (from the decrypted transcript in shadow mode), lets apply
// change it in Go and writes the result to every copy the project keeps. It
// reports whether the body changed. Call Prepare before beginning the
// transaction, and roll back on error. Non-body columns stay in the caller's
// own statements in the same transaction.
func (t *Transcripts) UpdateBody(ctx context.Context, db repo.DBTX, organizationID string, projectID, chatID, messageID uuid.UUID, apply func(*transcriptcrypto.Message) error) (bool, error) {
	var store *Store
	switch t.Mode(projectID) {
	case ModePlaintext:
	case ModeShadow:
		store = t.store
	case ModeEncrypted:
		return false, fmt.Errorf("update chat message body: %w", ErrUnsupported)
	}
	if store == nil {
		changed, err := updateBody(ctx, db, nil, organizationID, projectID, chatID, messageID, apply)
		if err != nil {
			return false, fmt.Errorf("update chat message body: %w", err)
		}
		return changed, nil
	}
	changed, err := store.UpdateBody(ctx, db, organizationID, projectID, chatID, messageID, apply)
	if err != nil {
		return false, fmt.Errorf("update chat message body: %w", err)
	}
	return changed, nil
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
		b.assign(message)
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

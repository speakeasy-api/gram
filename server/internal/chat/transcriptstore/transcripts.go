package transcriptstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

// ErrNotEnabled means the project does not have encrypted transcripts, so it
// has nothing to backfill or measure. Sealing copies for such a project would
// leave encrypted copies that go stale on its next write.
var ErrNotEnabled = errors.New("project does not have encrypted transcripts")

// Transcripts is the one entry point for the bodies of chat messages. It owns
// the decision a caller would otherwise repeat: whether a project has encrypted
// transcripts, so its messages are sealed on write and opened on read. Today
// that is the projects on GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS. Every other
// project reads plaintext: Seal and Open do nothing for it. A nil *Transcripts
// means no project has encrypted transcripts.
type Transcripts struct {
	store     *Store
	projects  map[uuid.UUID]struct{}
	encrypted map[uuid.UUID]struct{}
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
	return &Transcripts{store: store, projects: projects, encrypted: map[uuid.UUID]struct{}{}}
}

// WithEncryptedProjects puts projects in encrypted mode: the transcript is the
// only copy of each body, and the plaintext body columns are written empty.
// Nothing in configuration selects it yet; message inserts, reads and the
// remaining plaintext SQL (search, counts, lookups) must all move to the
// transcript before a real project can use it.
func (t *Transcripts) WithEncryptedProjects(projectIDs ...uuid.UUID) *Transcripts {
	if t.store == nil && len(projectIDs) > 0 {
		panic("transcriptstore: WithEncryptedProjects needs a store")
	}
	for _, projectID := range projectIDs {
		t.encrypted[projectID] = struct{}{}
	}
	return t
}

// Enabled reports whether the project has encrypted transcripts, in shadow or
// encrypted mode.
func (t *Transcripts) Enabled(projectID uuid.UUID) bool {
	return t.Mode(projectID) != ModePlaintext
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
	if t.Mode(projectID) != ModeEncrypted {
		return nil
	}
	if err := emptyPlaintext(ctx, repo.New(db), organizationID, projectID, messageIDs); err != nil {
		return fmt.Errorf("empty sealed plaintext: %w", err)
	}
	return nil
}

// Backfill seals one page of the project's existing messages after the
// cursor, with the same metadata as a live Seal. The page commits whole or
// not at all: on error nothing is sealed and the cursor does not advance, so
// the caller resumes from the same cursor. It returns ErrNotEnabled for a
// project that does not have encrypted transcripts.
func (t *Transcripts) Backfill(ctx context.Context, organizationID string, projectID, after uuid.UUID, limit int32) (Page, error) {
	if !t.Enabled(projectID) {
		return Page{After: after, Count: 0, More: false}, ErrNotEnabled
	}
	page, err := t.store.Backfill(ctx, organizationID, projectID, after, limit)
	if err != nil {
		return page, fmt.Errorf("backfill encrypted transcripts: %w", err)
	}
	return page, nil
}

// Progress reports how many of the project's messages have current encrypted
// transcripts. It returns ErrNotEnabled for a project that does not have
// encrypted transcripts.
func (t *Transcripts) Progress(ctx context.Context, organizationID string, projectID uuid.UUID) (Progress, error) {
	if !t.Enabled(projectID) {
		return Progress{Current: 0, Waiting: 0, Unattributed: 0}, ErrNotEnabled
	}
	progress, err := t.store.Progress(ctx, organizationID, projectID)
	if err != nil {
		return progress, fmt.Errorf("read encrypted transcript coverage: %w", err)
	}
	return progress, nil
}

// Mode is how a project stores message bodies.
type Mode int

const (
	// ModePlaintext keeps bodies in the plaintext columns only.
	ModePlaintext Mode = iota
	// ModeShadow keeps the plaintext columns and an encrypted transcript of
	// the same body. Reads open the transcript; writes keep both current.
	ModeShadow
	// ModeEncrypted keeps the body only in the encrypted transcript; the
	// plaintext body columns are empty. See WithEncryptedProjects.
	ModeEncrypted
)

// Mode reports how the project stores message bodies.
func (t *Transcripts) Mode(projectID uuid.UUID) Mode {
	if t == nil || t.store == nil {
		return ModePlaintext
	}
	if _, ok := t.encrypted[projectID]; ok {
		return ModeEncrypted
	}
	if _, ok := t.projects[projectID]; ok {
		return ModeShadow
	}
	return ModePlaintext
}

// UpdateMessage is the one way to change an existing message's body: content,
// content_raw, tool_calls, tool_outcome_notes, tool_call_summaries and
// storage_error. Inside the caller's transaction it locks the row and its
// transcript, hands apply the whole row with its body filled in (from the
// decrypted transcript outside plaintext mode), and writes what apply changed
// to every copy the project keeps. Besides the body, apply may change only
// tool_outcome, finish_reason, content_hash and risk_analyzed_at; any other
// change fails. It reports whether anything changed. Call Prepare before
// beginning the transaction, and roll back on error.
func (t *Transcripts) UpdateMessage(ctx context.Context, db repo.DBTX, organizationID string, projectID, chatID, messageID uuid.UUID, apply func(*chatrepo.ChatMessage) error) (bool, error) {
	mode := t.Mode(projectID)
	if mode == ModePlaintext {
		changed, err := updateMessage(ctx, db, nil, false, organizationID, projectID, chatID, messageID, apply)
		if err != nil {
			return false, fmt.Errorf("update chat message: %w", err)
		}
		return changed, nil
	}
	changed, err := t.store.updateMessageIn(ctx, db, mode == ModeEncrypted, organizationID, projectID, chatID, messageID, apply)
	if err != nil {
		return false, fmt.Errorf("update chat message: %w", err)
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

// Open replaces each body with its decrypted transcript, in one query per
// page; in plaintext mode it reads no transcript. In shadow mode a message with
// no transcript yet keeps its plaintext and counts as plaintext_fallback, and a
// transcript that disagrees with its plaintext row is logged, counted as stale,
// and the plaintext is kept, the same rule UpdateMessage follows. In encrypted
// mode the transcript is served without comparison, and a message without one
// fails the read with ErrMissingTranscript. A transcript that cannot be
// decrypted fails the read in either mode.
func (t *Transcripts) Open(ctx context.Context, organizationID string, projectID uuid.UUID, bodies []Body) error {
	if len(bodies) == 0 || !t.Enabled(projectID) {
		return nil
	}
	ids := make([]uuid.UUID, len(bodies))
	for i, b := range bodies {
		ids[i] = b.MessageID
	}
	opened, err := t.store.open(ctx, organizationID, projectID, ids, t.Mode(projectID) == ModeEncrypted)
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

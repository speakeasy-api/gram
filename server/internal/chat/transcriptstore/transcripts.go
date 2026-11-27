package transcriptstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
)

// Transcripts is the one entry point for the protected fields of chat messages. It owns
// the decision a caller would otherwise repeat: whether a project has encrypted
// transcripts, so its messages are sealed on write and opened on read. Today
// that is the projects on GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS. Every other
// project reads plaintext: inserts, updates and Open touch no transcript. A nil *Transcripts
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
// only copy of each message's protected fields, and the plaintext columns are written empty.
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
// before the caller opens the transaction that an insert or UpdateChatMessage
// will use, so neither makes a KMS call while the transaction is open. An
// error means they would fail too. InTx calls it for you.
func (t *Transcripts) Prepare(ctx context.Context, organizationID string, projectID uuid.UUID) error {
	if !t.Enabled(projectID) {
		return nil
	}
	if err := t.store.Prepare(ctx, organizationID); err != nil {
		return fmt.Errorf("prepare encrypted chat transcript: %w", err)
	}
	return nil
}

// Mode is how a project stores protected fields.
type Mode int

const (
	// ModePlaintext keeps protected fields in the plaintext columns only.
	ModePlaintext Mode = iota
	// ModeShadow keeps the plaintext columns and an encrypted transcript of
	// the same protected fields. Reads open the transcript; writes keep both current.
	ModeShadow
	// ModeEncrypted keeps the protected fields only in the encrypted transcript; the
	// plaintext protected fields are empty. See WithEncryptedProjects.
	ModeEncrypted
)

// Mode reports how the project stores protected fields.
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

// UpdateChatMessage is the one way to change an existing message's protected fields: content,
// content_raw, tool_calls, tool_outcome_notes, tool_call_summaries and
// storage_error. Inside the caller's transaction it locks the row and its
// transcript, hands apply the whole row with its protected fields filled in (from the
// decrypted transcript outside plaintext mode), and writes what apply changed
// to every copy the project keeps. Besides the protected fields, apply may change only
// tool_outcome, finish_reason, content_hash and risk_analyzed_at; any other
// change fails. It reports whether anything changed. Call Prepare before
// beginning the transaction, and roll back on error.
func (t *Transcripts) UpdateChatMessage(ctx context.Context, db repo.DBTX, organizationID string, projectID, chatID, messageID uuid.UUID, apply func(*chatrepo.ChatMessage) error) (bool, error) {
	p, err := t.protector(db, organizationID, projectID)
	if err != nil {
		return false, fmt.Errorf("update chat message: %w", err)
	}
	changed, err := p.update(ctx, chatID, messageID, apply)
	if err != nil {
		return false, fmt.Errorf("update chat message: %w", err)
	}
	return changed, nil
}

// OpenTarget is one loaded message for Open: its ID, and Set, which writes
// the opened protected fields into the row the caller loaded. Each row type's
// Set writes only the fields that row has.
type OpenTarget struct {
	MessageID uuid.UUID
	Set       func(transcriptcrypto.ProtectedFields)
}

// Open writes each message's protected fields into its target, in one query
// per page; in plaintext mode it reads no transcript. In shadow mode a message
// with no transcript yet gets its plaintext and counts as plaintext_fallback,
// and a transcript that disagrees with its plaintext row is logged, counted as
// stale, and the plaintext is served, the same rule UpdateChatMessage follows.
// In encrypted mode the transcript is served without comparison, and a message
// without one fails the read with ErrMissingTranscript. A transcript that
// cannot be decrypted fails the read in either mode.
func (t *Transcripts) Open(ctx context.Context, organizationID string, projectID uuid.UUID, targets []OpenTarget) error {
	if len(targets) == 0 || !t.Enabled(projectID) {
		return nil
	}
	ids := make([]uuid.UUID, len(targets))
	for i, target := range targets {
		ids[i] = target.MessageID
	}
	opened, err := t.store.open(ctx, organizationID, projectID, ids, t.Mode(projectID) == ModeEncrypted)
	if err != nil {
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	for _, target := range targets {
		// A message outside the project or organization is not opened; the
		// caller's page query is project-scoped, so this does not happen.
		if o, ok := opened[target.MessageID]; ok {
			target.Set(o.fields)
		}
	}
	return nil
}

// OpenRows is Open for a page of rows of any sqlc row type. targetOf maps a
// row to its target.
func OpenRows[T any](ctx context.Context, t *Transcripts, organizationID string, projectID uuid.UUID, rows []T, targetOf func(*T) OpenTarget) error {
	// Plaintext reads build no targets.
	if len(rows) == 0 || !t.Enabled(projectID) {
		return nil
	}
	targets := make([]OpenTarget, len(rows))
	for i := range rows {
		targets[i] = targetOf(&rows[i])
	}
	return t.Open(ctx, organizationID, projectID, targets)
}

// WithProtectedFields returns the row with its six protected fields set from
// fields. A full chat_messages row's OpenTarget uses it as its Set.
func WithProtectedFields(m chatrepo.ChatMessage, fields transcriptcrypto.ProtectedFields) chatrepo.ChatMessage {
	m.Content = fields.Content
	m.ContentRaw = fields.ContentRaw
	m.ToolCalls = fields.ToolCalls
	m.ToolOutcomeNotes = optionalText(fields.ToolOutcomeNotes)
	m.ToolCallSummaries = fields.ToolCallSummaries
	m.StorageError = optionalText(fields.StorageError)
	return m
}

func optionalText(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: *v, Valid: true}
}

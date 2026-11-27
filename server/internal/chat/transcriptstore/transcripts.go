package transcriptstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// Transcripts is the one entry point for the protected fields of chat
// messages. It owns the decision a caller would otherwise repeat: whether a
// project has encrypted transcripts, so its messages are sealed on write and
// opened on read. Mode is that decision and nothing else reads its inputs: the
// projects on GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS, and the organizations
// feature.FlagChatTranscriptShadow is on for (see WithFeatureFlag). Every other
// project reads plaintext: inserts, updates and OpenChatMessages touch no
// transcript. A nil *Transcripts means no project has encrypted transcripts.
type Transcripts struct {
	store     *Store
	projects  map[uuid.UUID]struct{}
	encrypted map[uuid.UUID]struct{}
	flag      *flagGate
}

// NewTranscripts encrypts transcripts for the listed projects, whatever the
// feature flag says. An empty list and no WithFeatureFlag turns every method
// into a no-op; there is no implicit all-project mode. Listed projects without
// a store are a wiring mistake that would silently skip encryption, so it
// panics.
func NewTranscripts(store *Store, projectIDs ...uuid.UUID) *Transcripts {
	if store == nil && len(projectIDs) > 0 {
		panic("transcriptstore: NewTranscripts needs a store when projects are listed")
	}
	projects := make(map[uuid.UUID]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		projects[projectID] = struct{}{}
	}
	return &Transcripts{store: store, projects: projects, encrypted: map[uuid.UUID]struct{}{}, flag: nil}
}

// WithFeatureFlag also puts in shadow mode every project of an organization
// feature.FlagChatTranscriptShadow is on for. The flag is evaluated from the
// provider's locally cached definitions only, so no read or write calls
// PostHog. Only a writer's Prepare provisions such an organization's transcript
// key, outside any transaction; until that succeeds in this process the
// organization stays plaintext for writes, so a key that cannot be provisioned
// never fails a write. Once it is provisioned the project is in shadow mode,
// and sealing fails closed like for a listed project. Reads never provision a
// key: Open reads whatever transcripts exist, and an organization without a
// key has none, so its messages are served from plaintext. An error or an
// undecided lookup of the project's organization or of the flag reads as
// plaintext.
func (t *Transcripts) WithFeatureFlag(features feature.Provider, provisioner Provisioner) *Transcripts {
	if t.store == nil || features == nil || provisioner == nil {
		panic("transcriptstore: WithFeatureFlag needs a store, a feature provider and a provisioner")
	}
	t.flag = newFlagGate(features, provisioner, t.store.logger)
	return t
}

// WithEncryptedProjects puts projects in encrypted mode: the transcript is the
// only copy of each message's protected fields, and the plaintext columns are written empty.
// Nothing in configuration selects it yet; message inserts, reads and the
// remaining plaintext SQL (search, counts, lookups) must all move to the
// transcript before a real project can use it. The feature flag never selects it.
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
func (t *Transcripts) Enabled(ctx context.Context, projectID uuid.UUID) bool {
	return t.Mode(ctx, projectID) != ModePlaintext
}

// MaySeal reports whether a write to the project may seal a transcript, so
// the caller must pass its organization to InTx: the project is in shadow or
// encrypted mode, or the feature flag puts it in shadow mode once Prepare
// provisions its organization's key.
func (t *Transcripts) MaySeal(ctx context.Context, projectID uuid.UUID) bool {
	if t == nil || t.store == nil {
		return false
	}
	d := t.decide(ctx, t.store.db, projectID)
	return d.mode != ModePlaintext || d.unprovisioned
}

// Prepare loads the organization's transcript key into the process cache
// before the caller opens the transaction that an insert or UpdateChatMessage
// will use, so neither makes a KMS call while the transaction is open. For a
// project the feature flag puts in shadow mode it first provisions the
// organization's key, once per process. An error means they would fail too.
// Begin calls it for you.
func (t *Transcripts) Prepare(ctx context.Context, organizationID string, projectID uuid.UUID) error {
	if t.activate(ctx, projectID) == ModePlaintext {
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

// modeRules is everything that depends on a project's mode, in one place, so
// a new mode is one new value and no call site changes. Inserts, updates,
// reads and reseals ask these questions instead of checking the mode.
type modeRules struct {
	// sealsTranscript: every write also stores an encrypted transcript, and
	// reads and updates open it.
	sealsTranscript bool
	// writesPlaintextColumns: the protected fields are also written to the
	// chat_messages columns. When false the columns are written empty.
	writesPlaintextColumns bool
	// comparesTranscriptToPlaintext: an opened transcript is checked against
	// the plaintext row, and the plaintext row is served when they disagree.
	comparesTranscriptToPlaintext bool
	// missingTranscriptError is returned when a message has no current
	// transcript. Nil serves the plaintext row instead.
	missingTranscriptError error
}

var (
	plaintextRules = modeRules{sealsTranscript: false, writesPlaintextColumns: true, comparesTranscriptToPlaintext: false, missingTranscriptError: nil}
	shadowRules    = modeRules{sealsTranscript: true, writesPlaintextColumns: true, comparesTranscriptToPlaintext: true, missingTranscriptError: nil}
	encryptedRules = modeRules{sealsTranscript: true, writesPlaintextColumns: false, comparesTranscriptToPlaintext: false, missingTranscriptError: ErrMissingTranscript}
)

// rules returns the mode's rules.
func (m Mode) rules() modeRules {
	if m == ModeEncrypted {
		return encryptedRules
	}
	if m == ModeShadow {
		return shadowRules
	}
	return plaintextRules
}

// Mode reports how the project stores protected fields. It is the only place
// that reads the project lists and the feature flag.
func (t *Transcripts) Mode(ctx context.Context, projectID uuid.UUID) Mode {
	if t == nil || t.store == nil {
		return ModePlaintext
	}
	return t.decide(ctx, t.store.db, projectID).mode
}

// decision is Mode with what the flag path learned.
type decision struct {
	mode Mode
	// organizationID is the project's organization when the flag decided.
	organizationID string
	// unprovisioned is set when the feature flag puts the project in shadow
	// mode but this process has not provisioned its organization's key yet.
	unprovisioned bool
}

// decide reads the project lists and the feature flag. db is the caller's
// transaction when it has one.
func (t *Transcripts) decide(ctx context.Context, db repo.DBTX, projectID uuid.UUID) decision {
	plaintext := decision{mode: ModePlaintext, organizationID: "", unprovisioned: false}
	if t == nil || t.store == nil {
		return plaintext
	}
	if _, ok := t.encrypted[projectID]; ok {
		return decision{mode: ModeEncrypted, organizationID: "", unprovisioned: false}
	}
	if _, ok := t.projects[projectID]; ok {
		return decision{mode: ModeShadow, organizationID: "", unprovisioned: false}
	}
	if t.flag == nil {
		return plaintext
	}
	org, ok := t.flag.organization(ctx, db, projectID)
	if !ok || !t.flag.enabled(ctx, org) {
		return plaintext
	}
	if !t.flag.isProvisioned(org.id) {
		return decision{mode: ModePlaintext, organizationID: org.id, unprovisioned: true}
	}
	return decision{mode: ModeShadow, organizationID: org.id, unprovisioned: false}
}

// activate provisions the organization of a project the feature flag puts in
// shadow mode, outside any transaction, and returns the project's mode after.
// Only the write path calls it.
func (t *Transcripts) activate(ctx context.Context, projectID uuid.UUID) Mode {
	if t == nil || t.store == nil {
		return ModePlaintext
	}
	d := t.decide(ctx, t.store.db, projectID)
	if !d.unprovisioned {
		return d.mode
	}
	if !t.flag.ensureProvisioned(ctx, d.organizationID) {
		return ModePlaintext
	}
	return ModeShadow
}

// readMode is the mode reads use. A project the feature flag puts in shadow
// mode reads its transcripts even before this process provisioned the key: a
// transcript exists only if a writer provisioned the key, so reads never need
// to create one.
func (t *Transcripts) readMode(ctx context.Context, projectID uuid.UUID) Mode {
	if t == nil || t.store == nil {
		return ModePlaintext
	}
	d := t.decide(ctx, t.store.db, projectID)
	if d.unprovisioned {
		return ModeShadow
	}
	return d.mode
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
	p, err := t.protector(ctx, db, organizationID, projectID)
	if err != nil {
		return false, fmt.Errorf("update chat message: %w", err)
	}
	changed, err := p.update(ctx, chatID, messageID, apply)
	if err != nil {
		return false, fmt.Errorf("update chat message: %w", err)
	}
	return changed, nil
}

// OpenTarget is one loaded message for OpenChatMessages: its ID, and Set, which writes
// the opened protected fields into the row the caller loaded. Each row type's
// Set writes only the fields that row has.
type OpenTarget struct {
	MessageID uuid.UUID
	Set       func(transcriptcrypto.ProtectedFields)
}

// open writes each message's protected fields into its target, in one query
// per page; in plaintext mode it reads no transcript. In shadow mode a message
// with no transcript yet gets its plaintext and counts as plaintext_fallback,
// and a transcript that disagrees with its plaintext row is logged, counted as
// stale, and the plaintext is served, the same rule UpdateChatMessage follows.
// In encrypted mode the transcript is served without comparison, and a message
// without one fails the read with ErrMissingTranscript. A transcript that
// cannot be decrypted fails the read in either mode. open never provisions a
// key.
//
// A message edited while the flag was off for its organization keeps the
// transcript sealed before the edit. When the flag is back on, reads of it log
// the disagreement as an error, count it as stale and serve the plaintext, and
// its next update or a backfill reseals it. Those errors are left as errors on
// purpose: from the rows alone they cannot be told apart from a write that
// skipped sealing, which is a bug. Run a backfill for the organization's
// projects after turning the flag back on to clear them.
func (t *Transcripts) open(ctx context.Context, organizationID string, projectID uuid.UUID, targets []OpenTarget) error {
	if len(targets) == 0 {
		return nil
	}
	mode := t.readMode(ctx, projectID)
	if mode == ModePlaintext {
		return nil
	}
	ids := make([]uuid.UUID, len(targets))
	for i, target := range targets {
		ids[i] = target.MessageID
	}
	opened, err := t.store.open(ctx, organizationID, projectID, ids, mode.rules())
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

// OpenChatMessages opens a page of loaded messages of any sqlc row type; see open for
// the fallback and failure rules. targetOf maps a
// row to its target.
func OpenChatMessages[T any](ctx context.Context, t *Transcripts, organizationID string, projectID uuid.UUID, rows []T, targetOf func(*T) OpenTarget) error {
	// Plaintext reads build no targets. This is open's read mode, so a reader
	// opens transcripts another process sealed for a flag-enabled organization.
	if len(rows) == 0 || t.readMode(ctx, projectID) == ModePlaintext {
		return nil
	}
	targets := make([]OpenTarget, len(rows))
	for i := range rows {
		targets[i] = targetOf(&rows[i])
	}
	return t.open(ctx, organizationID, projectID, targets)
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

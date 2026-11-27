package transcriptstore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// Transcripts is the one entry point for the protected fields of chat
// messages. It owns the decision a caller would otherwise repeat: whether a
// project has encrypted transcripts, so its messages are sealed on write and
// opened on read. Mode is that decision and nothing else reads its inputs: the
// projects on GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS, and the organizations
// feature.FlagChatTranscriptShadow is on for (see WithFeatureFlag). Every other
// project reads plaintext: Seal and Open do nothing for it. A nil *Transcripts
// means no project has encrypted transcripts.
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
// PostHog. The first Prepare or Open for such an organization provisions its
// transcript key outside any transaction; until that succeeds in this process
// the organization stays plaintext, so a key that cannot be provisioned never
// fails a write. Once it is provisioned the project is in shadow mode, and
// sealing fails closed like for a listed project. An error or an undecided
// lookup of the project's organization or of the flag reads as plaintext.
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

// Prepare loads the organization's transcript key into the process cache
// before the caller opens the transaction that Seal will use, so Seal makes no
// KMS call while the transaction is open. For a project the feature flag puts
// in shadow mode it first provisions the organization's key, once per process.
// An error means Seal would fail too.
func (t *Transcripts) Prepare(ctx context.Context, organizationID string, projectID uuid.UUID) error {
	if t.activate(ctx, projectID) == ModePlaintext {
		return nil
	}
	if err := t.store.Prepare(ctx, organizationID); err != nil {
		return fmt.Errorf("prepare encrypted chat transcript: %w", err)
	}
	return nil
}

// PrepareProject is Prepare for a caller that has only the project.
func (t *Transcripts) PrepareProject(ctx context.Context, projectID uuid.UUID) error {
	if t == nil || t.store == nil {
		return nil
	}
	if d := t.decide(ctx, t.store.db, projectID); d.mode == ModePlaintext && d.unprovisioned == "" {
		return nil
	}
	org, err := repo.New(t.store.db).GetProjectOrganization(ctx, projectID)
	if err != nil {
		return fmt.Errorf("get chat transcript project organization: %w", err)
	}
	return t.Prepare(ctx, org.OrganizationID, projectID)
}

// Seal stores the encrypted copy of messages the caller just wrote, inside the
// caller's transaction. Call Prepare before beginning that transaction. The
// caller must roll back on error.
func (t *Transcripts) Seal(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) error {
	mode := t.decide(ctx, db, projectID).mode
	if mode == ModePlaintext {
		return nil
	}
	encrypted := mode == ModeEncrypted
	if err := t.store.captureIn(ctx, db, encrypted, organizationID, projectID, messageIDs); err != nil {
		return fmt.Errorf("seal chat transcript: %w", err)
	}
	if !encrypted {
		return nil
	}
	if err := emptyPlaintext(ctx, repo.New(db), organizationID, projectID, messageIDs); err != nil {
		return fmt.Errorf("empty sealed plaintext: %w", err)
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

// Mode reports how the project stores protected fields. It is the only place
// that reads the project lists and the feature flag.
func (t *Transcripts) Mode(ctx context.Context, projectID uuid.UUID) Mode {
	if t == nil || t.store == nil {
		return ModePlaintext
	}
	return t.decide(ctx, t.store.db, projectID).mode
}

// decision is Mode with the reason a flag-enabled project is still plaintext.
type decision struct {
	mode Mode
	// unprovisioned is set when the feature flag puts the project in shadow
	// mode but this process has not provisioned its organization's key yet.
	unprovisioned string
}

// decide reads the project lists and the feature flag. db is the caller's
// transaction when it has one.
func (t *Transcripts) decide(ctx context.Context, db repo.DBTX, projectID uuid.UUID) decision {
	plaintext := decision{mode: ModePlaintext, unprovisioned: ""}
	if t == nil || t.store == nil {
		return plaintext
	}
	if _, ok := t.encrypted[projectID]; ok {
		return decision{mode: ModeEncrypted, unprovisioned: ""}
	}
	if _, ok := t.projects[projectID]; ok {
		return decision{mode: ModeShadow, unprovisioned: ""}
	}
	if t.flag == nil {
		return plaintext
	}
	org, ok := t.flag.organization(ctx, db, projectID)
	if !ok || !t.flag.enabled(ctx, org) {
		return plaintext
	}
	if !t.flag.isProvisioned(org.id) {
		return decision{mode: ModePlaintext, unprovisioned: org.id}
	}
	return decision{mode: ModeShadow, unprovisioned: ""}
}

// activate provisions the organization of a project the feature flag puts in
// shadow mode, outside any transaction, and returns the project's mode after.
func (t *Transcripts) activate(ctx context.Context, projectID uuid.UUID) Mode {
	if t == nil || t.store == nil {
		return ModePlaintext
	}
	d := t.decide(ctx, t.store.db, projectID)
	if d.unprovisioned == "" {
		return d.mode
	}
	if !t.flag.ensureProvisioned(ctx, d.unprovisioned) {
		return ModePlaintext
	}
	return ModeShadow
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
	mode := t.decide(ctx, db, projectID).mode
	if mode == ModePlaintext {
		changed, err := updateChatMessage(ctx, db, nil, false, organizationID, projectID, chatID, messageID, apply)
		if err != nil {
			return false, fmt.Errorf("update chat message: %w", err)
		}
		return changed, nil
	}
	changed, err := t.store.updateChatMessageIn(ctx, db, mode == ModeEncrypted, organizationID, projectID, chatID, messageID, apply)
	if err != nil {
		return false, fmt.Errorf("update chat message: %w", err)
	}
	return changed, nil
}

// ProtectedFieldRefs points at the protected fields of one loaded message, so Open can replace
// them with the opened copy whatever query loaded the row. A nil field was
// not loaded and is left alone.
type ProtectedFieldRefs struct {
	MessageID         uuid.UUID
	Content           *string
	ContentRaw        *[]byte
	ToolCalls         *[]byte
	ToolOutcomeNotes  *pgtype.Text
	ToolCallSummaries *[]byte
	StorageError      *pgtype.Text
}

// Open replaces each message's protected fields with its decrypted transcript, in one query per
// page; in plaintext mode it reads no transcript. In shadow mode a message with
// no transcript yet keeps its plaintext and counts as plaintext_fallback, and a
// transcript that disagrees with its plaintext row is logged, counted as stale,
// and the plaintext is kept, the same rule UpdateChatMessage follows. In encrypted
// mode the transcript is served without comparison, and a message without one
// fails the read with ErrMissingTranscript. A transcript that cannot be
// decrypted fails the read in either mode. For a project the feature flag puts
// in shadow mode it first provisions the organization's key, once per process.
//
// A message edited while the flag was off for its organization keeps the
// transcript sealed before the edit. When the flag is back on, reads of it log
// the disagreement as an error, count it as stale and serve the plaintext, and
// its next update or a backfill reseals it. Those errors are left as errors on
// purpose: from the rows alone they cannot be told apart from a write that
// skipped sealing, which is a bug. Run a backfill for the organization's
// projects after turning the flag back on to clear them.
func (t *Transcripts) Open(ctx context.Context, organizationID string, projectID uuid.UUID, refs []ProtectedFieldRefs) error {
	if len(refs) == 0 {
		return nil
	}
	mode := t.activate(ctx, projectID)
	if mode == ModePlaintext {
		return nil
	}
	ids := make([]uuid.UUID, len(refs))
	for i, b := range refs {
		ids[i] = b.MessageID
	}
	opened, err := t.store.open(ctx, organizationID, projectID, ids, mode == ModeEncrypted)
	if err != nil {
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	for _, b := range refs {
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

// OpenRows is Open for a page of rows of any sqlc row type. refsOf maps a row
// to pointers at its protected fields.
func OpenRows[T any](ctx context.Context, t *Transcripts, organizationID string, projectID uuid.UUID, rows []T, refsOf func(*T) ProtectedFieldRefs) error {
	if len(rows) == 0 {
		return nil
	}
	refs := make([]ProtectedFieldRefs, len(rows))
	for i := range rows {
		refs[i] = refsOf(&rows[i])
	}
	return t.Open(ctx, organizationID, projectID, refs)
}

func optionalText(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: *v, Valid: true}
}

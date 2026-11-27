package transcriptstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// The insert methods below are the only way to store new chat messages. Each
// runs its INSERT in the caller's transaction and keeps every copy the
// project's mode needs, with the same steps in every mode:
//
//   - the protected fields are read from the params, once;
//   - the INSERT writes them, or empty columns in encrypted mode;
//   - outside plaintext mode, each row the INSERT created is sealed from
//     those fields, in the text form Postgres stores for jsonb.
//
// The params are taken by value, so the caller's copy is never changed. Run
// them inside InTx, or call Prepare before beginning the transaction, and roll
// back on error.

// InsertChatMessages stores new messages with one COPY. The caller supplies
// every ID, so every row is new.
func (t *Transcripts) InsertChatMessages(ctx context.Context, db chatrepo.DBTX, organizationID string, projectID uuid.UUID, params []chatrepo.CreateChatMessageParams) (n int64, err error) {
	if len(params) == 0 {
		return 0, nil
	}
	rows := slices.Clone(params)
	news := make([]newMessage, len(rows))
	for i, r := range rows {
		news[i] = newMessage{id: r.ID, chatID: r.ChatID, fields: insertedFields(r.Content, r.ContentRaw, r.ToolCalls, r.StorageError)}
	}
	s, err := t.newSealer(ctx, db, organizationID, projectID, news)
	if err != nil {
		return 0, err
	}
	defer s.end(ctx, &err)

	if s.blank {
		for i := range rows {
			blank(&rows[i].Content, &rows[i].ContentRaw, &rows[i].ToolCalls, &rows[i].StorageError)
		}
	}
	n, err = chatrepo.New(db).CreateChatMessage(ctx, rows)
	if err != nil {
		return 0, fmt.Errorf("create chat messages: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	if err := s.seal(ctx, ids); err != nil {
		return 0, err
	}
	return n, nil
}

// InsertExternalChatMessage stores an imported message unless one with the
// same external message ID is already in the chat. It reports whether it
// inserted.
func (t *Transcripts) InsertExternalChatMessage(ctx context.Context, db chatrepo.DBTX, organizationID string, projectID uuid.UUID, params chatrepo.CreateExternalChatMessageParams) (inserted bool, err error) {
	news := []newMessage{{id: params.ID, chatID: params.ChatID, fields: insertedFields(params.Content, params.ContentRaw, params.ToolCalls, params.StorageError)}}
	s, err := t.newSealer(ctx, db, organizationID, projectID, news)
	if err != nil {
		return false, err
	}
	defer s.end(ctx, &err)

	if s.blank {
		blank(&params.Content, &params.ContentRaw, &params.ToolCalls, &params.StorageError)
	}
	id, err := chatrepo.New(db).CreateExternalChatMessage(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create external chat message: %w", err)
	}
	if err := s.seal(ctx, []uuid.UUID{id}); err != nil {
		return false, err
	}
	return true, nil
}

// UpsertCorrelatedChatMessage stores a message, or promotes the earlier
// LiteLLM row for the same turn to the native-hook source. A promotion changes
// attribution only, so the protected fields and their transcript stay as they
// were. found is false when a row for the turn exists and cannot be promoted.
// The returned content and tool calls are the stored ones; for an inserted row
// they are the params' in every mode, for a promoted one the plaintext
// columns.
func (t *Transcripts) UpsertCorrelatedChatMessage(ctx context.Context, db chatrepo.DBTX, organizationID string, projectID uuid.UUID, params chatrepo.UpsertCorrelatedChatMessageParams) (row chatrepo.UpsertCorrelatedChatMessageRow, found bool, err error) {
	news := []newMessage{{id: params.ID, chatID: params.ChatID, fields: insertedFields(params.Content, params.ContentRaw, params.ToolCalls, params.StorageError)}}
	s, err := t.newSealer(ctx, db, organizationID, projectID, news)
	if err != nil {
		return row, false, err
	}
	defer s.end(ctx, &err)

	content, toolCalls := params.Content, params.ToolCalls
	if s.blank {
		blank(&params.Content, &params.ContentRaw, &params.ToolCalls, &params.StorageError)
	}
	row, err = chatrepo.New(db).UpsertCorrelatedChatMessage(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, fmt.Errorf("upsert correlated chat message: %w", err)
	}
	if !row.Inserted {
		return row, true, nil
	}
	if err := s.seal(ctx, []uuid.UUID{row.ID}); err != nil {
		return row, false, err
	}
	row.Content, row.ToolCalls = content, toolCalls
	return row, true, nil
}

// Beginner opens a transaction.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// InTx runs fn in a new transaction on db and commits it. It loads the
// project's transcript key before the transaction begins, so nothing inside fn
// calls KMS while the transaction is open, and a key that will not load fails
// the call before anything is written. An error from fn rolls back.
func (t *Transcripts) InTx(ctx context.Context, db Beginner, organizationID string, projectID uuid.UUID, fn func(pgx.Tx) error) error {
	if err := t.Prepare(ctx, organizationID, projectID); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// newMessage is a message an insert is about to store.
type newMessage struct {
	id, chatID uuid.UUID
	fields     transcriptcrypto.ProtectedFields
}

// sealer is what the project's mode asks of one insert: whether the plaintext
// columns are written empty, and which transcripts to seal once the rows
// exist. In plaintext mode it seals nothing.
type sealer struct {
	store          *Store
	blank          bool
	db             repo.DBTX
	codec          *transcriptcrypto.Codec
	organizationID string
	projectID      uuid.UUID
	fields         map[uuid.UUID]newMessage
	span           trace.Span
	start          time.Time
}

// newSealer builds the codec and the canonical fields before the INSERT runs,
// so a cold key fails before anything is written.
func (t *Transcripts) newSealer(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, news []newMessage) (*sealer, error) {
	mode := t.Mode(projectID)
	if mode == ModePlaintext {
		return &sealer{store: nil, blank: false, db: nil, codec: nil, organizationID: "", projectID: projectID, fields: nil, span: nil, start: time.Time{}}, nil
	}
	if db == nil || organizationID == "" || projectID == uuid.Nil {
		return nil, ErrInvalid
	}
	s := t.store
	sl := &sealer{store: s, blank: mode == ModeEncrypted, db: db, codec: nil, organizationID: organizationID, projectID: projectID, fields: make(map[uuid.UUID]newMessage, len(news)), span: s.startCapture(ctx, organizationID, projectID, len(news)), start: time.Now()}
	codec, err := transcriptcrypto.NewIn(s.service, db, organizationID)
	if err != nil {
		err = safe(err)
		sl.end(ctx, &err)
		return nil, err
	}
	sl.codec = codec
	q := repo.New(db)
	for _, m := range news {
		m.fields, err = canonical(ctx, q, m.fields)
		if err != nil {
			sl.end(ctx, &err)
			return nil, err
		}
		sl.fields[m.id] = m
	}
	return sl, nil
}

// startCapture opens the span for one insert; sealer.end closes it.
func (s *Store) startCapture(ctx context.Context, organizationID string, projectID uuid.UUID, count int) trace.Span {
	//nolint:spancheck // sealer.end ends the span once the insert finishes.
	_, span := s.tracer.Start(ctx, "transcriptstore.Capture", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowMessageCount(count),
	))
	return span //nolint:spancheck // sealer.end ends the span once the insert finishes.
}

// seal writes the transcript of each row the INSERT created. The INSERT
// locked those rows, and they hold exactly the fields the sealer read.
func (s *sealer) seal(ctx context.Context, ids []uuid.UUID) error {
	if s.store == nil {
		return nil
	}
	q := repo.New(s.db)
	for _, id := range ids {
		m, ok := s.fields[id]
		if !ok {
			// The INSERT stored a row the sealer has no fields for.
			return ErrInvalid
		}
		if err := seal(ctx, q, s.codec, s.organizationID, s.projectID, m.chatID, m.id, m.fields); err != nil {
			return err
		}
	}
	return nil
}

// end records the insert's metrics and closes its span.
func (s *sealer) end(ctx context.Context, errp *error) {
	if s.store == nil {
		return
	}
	err := *errp
	result := classify(err)
	s.store.metrics.capture.record(ctx, s.projectID, len(s.fields), time.Since(s.start), result)
	s.span.SetAttributes(attr.Outcome(result))
	if err != nil {
		s.span.RecordError(err)
		s.span.SetStatus(codes.Error, string(result))
	}
	s.span.End()
}

// insertedFields are the protected fields a new row is written with: an
// insert writes no tool outcome notes or summaries.
func insertedFields(content string, raw, toolCalls []byte, storageError pgtype.Text) transcriptcrypto.ProtectedFields {
	return message(content, raw, toolCalls, pgtype.Text{String: "", Valid: false}, nil, storageError)
}

// blank empties the protected fields an insert writes, as encrypted mode
// stores them.
func blank(content *string, raw, toolCalls *[]byte, storageError *pgtype.Text) {
	*content, *raw, *toolCalls = "", nil, nil
	*storageError = pgtype.Text{String: "", Valid: false}
}

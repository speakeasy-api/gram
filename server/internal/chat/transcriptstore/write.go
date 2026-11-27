package transcriptstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// Every write of a message's protected fields, insert or update, goes through
// a protector, so the two cannot drift apart:
//
//   - prepare reads the fields once, in the text form Postgres stores for
//     jsonb, measures their metadata, and decides what the columns hold: the
//     fields, or empty ones in encrypted mode;
//   - the caller's INSERT or UPDATE writes those columns and the metadata in
//     one statement;
//   - finish seals the same fields (outside plaintext mode) and replaces the
//     message's tool-call IDs.

// protector applies a project's mode to the protected fields of one write.
type protector struct {
	// store is nil in plaintext mode: nothing is sealed, opened or counted.
	store          *Store
	encrypted      bool
	q              *repo.Queries
	codec          *transcriptcrypto.Codec
	organizationID string
	projectID      uuid.UUID
}

// write is one message's protected fields as a write stores them.
type write struct {
	// stored is what the chat_messages columns hold.
	stored transcriptcrypto.ProtectedFields
	// sealed is what the transcript holds.
	sealed transcriptcrypto.ProtectedFields
	// meta is measured from sealed.
	meta metadata
}

func (t *Transcripts) protector(db repo.DBTX, organizationID string, projectID uuid.UUID) (*protector, error) {
	var store *Store
	if t != nil {
		store = t.store
	}
	return store.protector(db, organizationID, projectID, t.Mode(projectID))
}

// protector builds the codec from the cached key before anything is written,
// so a cold key fails the write first.
func (s *Store) protector(db repo.DBTX, organizationID string, projectID uuid.UUID, mode Mode) (*protector, error) {
	if db == nil || organizationID == "" || projectID == uuid.Nil {
		return nil, ErrInvalid
	}
	p := &protector{store: nil, encrypted: false, q: repo.New(db), codec: nil, organizationID: organizationID, projectID: projectID}
	if mode == ModePlaintext {
		return p, nil
	}
	codec, err := transcriptcrypto.NewIn(s.service, db, organizationID)
	if err != nil {
		return nil, safe(err)
	}
	p.store, p.encrypted, p.codec = s, mode == ModeEncrypted, codec
	return p, nil
}

// prepare reads each message's protected fields in the text form Postgres
// stores, with one query for the batch.
func (p *protector) prepare(ctx context.Context, fields []transcriptcrypto.ProtectedFields) ([]write, error) {
	canonical, err := canonicalAll(ctx, p.q, fields)
	if err != nil {
		return nil, err
	}
	writes := make([]write, len(canonical))
	for i, f := range canonical {
		stored := f
		if p.encrypted {
			stored = emptyProtectedFields
		}
		writes[i] = write{stored: stored, sealed: f, meta: metadataOf(f.Content, f.ContentRaw, f.ToolCalls)}
	}
	return writes, nil
}

// finish seals the message's transcript and replaces its tool-call IDs, once
// its row holds w.stored and w.meta. The row is locked by that write until the
// transaction ends.
func (p *protector) finish(ctx context.Context, chatID, messageID uuid.UUID, w write) error {
	if p.store != nil {
		if err := sealTranscript(ctx, p.q, p.codec, p.organizationID, p.projectID, chatID, messageID, w.sealed); err != nil {
			return err
		}
	}
	return replaceToolCallIDs(ctx, p.q, p.projectID, messageID, w.meta.toolCallIDs)
}

// observe runs op in a span named name and records it in the metrics pick
// chooses, as count messages. In plaintext mode it only runs op.
func (p *protector) observe(ctx context.Context, name string, pick func(*storeMetrics) operationMetrics, count int, op func(context.Context) error) error {
	return p.store.observe(ctx, name, pick, p.organizationID, p.projectID, count, op)
}

// observe runs op in a span named name and records its duration, outcome and
// message count in the metrics pick chooses. A nil store only runs op.
func (s *Store) observe(ctx context.Context, name string, pick func(*storeMetrics) operationMetrics, organizationID string, projectID uuid.UUID, count int, op func(context.Context) error) error {
	if s == nil {
		return op(ctx)
	}
	ctx, span := s.tracer.Start(ctx, name, trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowMessageCount(count),
	))
	defer span.End()

	start := time.Now()
	err := op(ctx)
	result := classify(err)
	pick(s.metrics).record(ctx, projectID, count, time.Since(start), result)

	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
	}
	return err
}

func captureMetrics(m *storeMetrics) operationMetrics { return m.capture }
func updateMetrics(m *storeMetrics) operationMetrics  { return m.update }

// InsertChatMessages stores new messages with one COPY, in the caller's
// transaction. The caller supplies every ID, so every row is new. The params
// are taken by value and never changed. Run it inside InTx, and roll back on
// error.
func (t *Transcripts) InsertChatMessages(ctx context.Context, db chatrepo.DBTX, organizationID string, projectID uuid.UUID, params []chatrepo.CreateChatMessageParams) (int64, error) {
	if len(params) == 0 {
		return 0, nil
	}
	p, err := t.protector(db, organizationID, projectID)
	if err != nil {
		return 0, err
	}
	var n int64
	err = p.observe(ctx, "transcriptstore.Capture", captureMetrics, len(params), func(ctx context.Context) error {
		fields := make([]transcriptcrypto.ProtectedFields, len(params))
		for i, r := range params {
			fields[i] = insertedFields(r.Content, r.ContentRaw, r.ToolCalls, r.StorageError)
		}
		writes, err := p.prepare(ctx, fields)
		if err != nil {
			return err
		}
		rows := make([]repo.InsertChatMessagesParams, len(params))
		for i, r := range params {
			rows[i] = insertRow(r, writes[i])
		}
		n, err = p.q.InsertChatMessages(ctx, rows)
		if err != nil {
			return fmt.Errorf("create chat messages: %w", err)
		}
		for i, r := range params {
			if err := p.finish(ctx, r.ChatID, r.ID, writes[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// InsertExternalChatMessage stores an imported message unless one with the
// same external message ID is already in the chat. It reports whether it
// inserted.
func (t *Transcripts) InsertExternalChatMessage(ctx context.Context, db chatrepo.DBTX, organizationID string, projectID uuid.UUID, params chatrepo.CreateExternalChatMessageParams) (bool, error) {
	p, err := t.protector(db, organizationID, projectID)
	if err != nil {
		return false, err
	}
	inserted := false
	err = p.observe(ctx, "transcriptstore.Capture", captureMetrics, 1, func(ctx context.Context) error {
		writes, err := p.prepare(ctx, []transcriptcrypto.ProtectedFields{insertedFields(params.Content, params.ContentRaw, params.ToolCalls, params.StorageError)})
		if err != nil {
			return err
		}
		id, err := p.q.InsertExternalChatMessage(ctx, externalRow(params, writes[0]))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("create external chat message: %w", err)
		}
		inserted = true
		return p.finish(ctx, params.ChatID, id, writes[0])
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

// UpsertCorrelatedChatMessage stores a message, or promotes the earlier
// LiteLLM row for the same turn to the native-hook source. A promotion changes
// attribution only, so the protected fields, their metadata and their
// transcript stay as they were. found is false when a row for the turn exists
// and cannot be promoted. For an inserted row the returned content and tool
// calls are the params', in every mode; for a promoted one they are its
// columns.
func (t *Transcripts) UpsertCorrelatedChatMessage(ctx context.Context, db chatrepo.DBTX, organizationID string, projectID uuid.UUID, params chatrepo.UpsertCorrelatedChatMessageParams) (chatrepo.UpsertCorrelatedChatMessageRow, bool, error) {
	var stored chatrepo.UpsertCorrelatedChatMessageRow
	p, err := t.protector(db, organizationID, projectID)
	if err != nil {
		return stored, false, err
	}
	found := false
	err = p.observe(ctx, "transcriptstore.Capture", captureMetrics, 1, func(ctx context.Context) error {
		writes, err := p.prepare(ctx, []transcriptcrypto.ProtectedFields{insertedFields(params.Content, params.ContentRaw, params.ToolCalls, params.StorageError)})
		if err != nil {
			return err
		}
		row, err := p.q.UpsertCorrelatedChatMessage(ctx, correlatedRow(params, writes[0]))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert correlated chat message: %w", err)
		}
		found = true
		stored = chatrepo.UpsertCorrelatedChatMessageRow(row)
		if !row.Inserted {
			return nil
		}
		stored.Content, stored.ToolCalls = params.Content, params.ToolCalls
		return p.finish(ctx, params.ChatID, row.ID, writes[0])
	})
	if err != nil {
		return stored, false, err
	}
	return stored, found, nil
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

// canonicalAll returns each message's fields with their JSON in the text form
// Postgres stores for jsonb, in one query.
func canonicalAll(ctx context.Context, q *repo.Queries, fields []transcriptcrypto.ProtectedFields) ([]transcriptcrypto.ProtectedFields, error) {
	type jsonFields struct {
		ContentRaw        json.RawMessage `json:"content_raw,omitempty"`
		ToolCalls         json.RawMessage `json:"tool_calls,omitempty"`
		ToolCallSummaries json.RawMessage `json:"tool_call_summaries,omitempty"`
	}
	batch := make([]jsonFields, len(fields))
	for i, f := range fields {
		batch[i] = jsonFields{ContentRaw: f.ContentRaw, ToolCalls: f.ToolCalls, ToolCallSummaries: f.ToolCallSummaries}
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("%w: protected fields hold invalid JSON", ErrInvalid)
	}
	rows, err := q.CanonicalJSON(ctx, encoded)
	if err != nil {
		return nil, safe(err)
	}
	if len(rows) != len(fields) {
		return nil, ErrInvalid
	}
	out := make([]transcriptcrypto.ProtectedFields, len(fields))
	for i, row := range rows {
		f := fields[row.Idx]
		f.ContentRaw, f.ToolCalls, f.ToolCallSummaries = orNil(row.ContentRaw), orNil(row.ToolCalls), orNil(row.ToolCallSummaries)
		out[i] = f
	}
	return out, nil
}

// orNil reads CanonicalJSON's empty string, which no JSON value is, as NULL.
func orNil(v string) []byte {
	if v == "" {
		return nil
	}
	return []byte(v)
}

// insertedFields are the protected fields a new row is written with: an
// insert writes no tool outcome notes or summaries.
func insertedFields(content string, raw, toolCalls []byte, storageError pgtype.Text) transcriptcrypto.ProtectedFields {
	return message(content, raw, toolCalls, pgtype.Text{String: "", Valid: false}, nil, storageError)
}

func insertRow(r chatrepo.CreateChatMessageParams, w write) repo.InsertChatMessagesParams {
	return repo.InsertChatMessagesParams{
		ID: r.ID, ChatID: r.ChatID, Role: r.Role, ProjectID: r.ProjectID,
		Content: w.stored.Content, ContentRaw: w.stored.ContentRaw, ContentAssetUrl: r.ContentAssetUrl,
		StorageError: optionalText(w.stored.StorageError), Model: r.Model, MessageID: r.MessageID,
		ToolCallID: r.ToolCallID, UserID: r.UserID, ExternalUserID: r.ExternalUserID,
		FinishReason: r.FinishReason, ToolCalls: w.stored.ToolCalls, PromptTokens: r.PromptTokens,
		CompletionTokens: r.CompletionTokens, TotalTokens: r.TotalTokens, Origin: r.Origin,
		UserAgent: r.UserAgent, IpAddress: r.IpAddress, Source: r.Source, ContentHash: r.ContentHash,
		Generation: r.Generation, Replayed: r.Replayed, CreatedAt: r.CreatedAt,
		ToolCallCount: w.meta.toolCallCount, ContentBytes: w.meta.contentBytes,
		ContentRawBytes: w.meta.contentRawBytes, ToolCallsBytes: w.meta.toolCallsBytes,
	}
}

func externalRow(r chatrepo.CreateExternalChatMessageParams, w write) repo.InsertExternalChatMessageParams {
	return repo.InsertExternalChatMessageParams{
		ID: r.ID, ChatID: r.ChatID, Role: r.Role, ProjectID: r.ProjectID,
		Content: w.stored.Content, ContentRaw: w.stored.ContentRaw, ContentAssetUrl: r.ContentAssetUrl,
		StorageError: optionalText(w.stored.StorageError), Model: r.Model, MessageID: r.MessageID,
		ToolCallID: r.ToolCallID, UserID: r.UserID, ExternalUserID: r.ExternalUserID,
		ExternalMessageID: r.ExternalMessageID, FinishReason: r.FinishReason, ToolCalls: w.stored.ToolCalls,
		PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens, TotalTokens: r.TotalTokens,
		Origin: r.Origin, UserAgent: r.UserAgent, IpAddress: r.IpAddress, Source: r.Source,
		ContentHash: r.ContentHash, Generation: r.Generation, CreatedAt: r.CreatedAt,
		ToolCallCount: w.meta.toolCallCount, ContentBytes: w.meta.contentBytes,
		ContentRawBytes: w.meta.contentRawBytes, ToolCallsBytes: w.meta.toolCallsBytes,
	}
}

func correlatedRow(r chatrepo.UpsertCorrelatedChatMessageParams, w write) repo.UpsertCorrelatedChatMessageParams {
	return repo.UpsertCorrelatedChatMessageParams{
		ID: r.ID, ChatID: r.ChatID, Role: r.Role, ProjectID: r.ProjectID,
		Content: w.stored.Content, ContentRaw: w.stored.ContentRaw, ContentAssetUrl: r.ContentAssetUrl,
		StorageError: optionalText(w.stored.StorageError), Model: r.Model, MessageID: r.MessageID,
		ToolCallID: r.ToolCallID, UserID: r.UserID, ExternalUserID: r.ExternalUserID,
		ExternalMessageID: r.ExternalMessageID, FinishReason: r.FinishReason, ToolCalls: w.stored.ToolCalls,
		PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens, TotalTokens: r.TotalTokens,
		Origin: r.Origin, UserAgent: r.UserAgent, IpAddress: r.IpAddress, Source: r.Source,
		ContentHash: r.ContentHash, Generation: r.Generation, Replayed: r.Replayed, CreatedAt: r.CreatedAt,
		ToolCallCount: w.meta.toolCallCount, ContentBytes: w.meta.contentBytes,
		ContentRawBytes: w.meta.contentRawBytes, ToolCallsBytes: w.meta.toolCallsBytes,
	}
}

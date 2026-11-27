// Package transcriptstore maintains encrypted transcripts. Coverage never
// implies plaintext removal; this package never updates message source fields.
package transcriptstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"reflect"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

var (
	ErrNotFound = errors.New("message or transcript not found")
	ErrStale    = errors.New("transcript is stale")
	ErrInvalid  = errors.New("invalid transcript store arguments")
	// ErrUnsupported reports a storage mode that is not available yet.
	ErrUnsupported = errors.New("transcript storage mode not supported yet")
	// ErrMissingTranscript reports an encrypted-mode message with no current
	// transcript: its protected fields exist nowhere else, so nothing can be served or
	// changed.
	ErrMissingTranscript = errors.New("message has no current encrypted transcript")
)

type Store struct {
	db      *pgxpool.Pool
	logger  *slog.Logger
	service *orgencryption.Service
	tracer  trace.Tracer
	metrics *storeMetrics
}

func New(db *pgxpool.Pool, service *orgencryption.Service, logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) (*Store, error) {
	if db == nil || service == nil || logger == nil || tracerProvider == nil || meterProvider == nil {
		return nil, ErrInvalid
	}
	metrics, err := newStoreMetrics(meterProvider)
	if err != nil {
		return nil, err
	}
	return &Store{
		db:      db,
		logger:  logger.With(attr.SlogComponent("transcriptstore")),
		service: service,
		tracer:  tracerProvider.Tracer(instrumentationName),
		metrics: metrics,
	}, nil
}

// operationError deliberately hides database/provider details while preserving errors.Is.
type operationError struct{ cause error }

func (e *operationError) Error() string { return "transcript store operation failed" }
func (e *operationError) Unwrap() error { return e.cause }
func safe(err error) error {
	if err == nil {
		return nil
	}
	return &operationError{cause: err}
}
func project(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }
func message(content string, raw, calls []byte, notes pgtype.Text, summaries []byte, storageError pgtype.Text) transcriptcrypto.ProtectedFields {
	m := transcriptcrypto.ProtectedFields{StorageError: nil, Content: content, ContentRaw: raw, ToolCalls: calls, ToolOutcomeNotes: nil, ToolCallSummaries: summaries}
	if storageError.Valid {
		m.StorageError = &storageError.String
	}
	if notes.Valid {
		m.ToolOutcomeNotes = &notes.String
	}
	return m
}

// Prepare unwraps the organization's active transcript key into the process's
// committed key cache through the pool. Call it before opening the transaction
// that Capture will use: Capture never calls KMS, and fails with
// orgencryption.ErrKeyNotWarm when the key is not cached. A warm cache needs
// no KMS call here either. A failed early refresh of a key that is still
// cached is not an error. Any error it returns means the key is not cached, so
// Capture would fail too. Capture still reads the configuration in its
// transaction and fails closed on its own.
func (s *Store) Prepare(ctx context.Context, organizationID string) error {
	if organizationID == "" {
		return ErrInvalid
	}
	ctx, span := s.tracer.Start(ctx, "transcriptstore.Prepare", trace.WithAttributes(attr.OrganizationID(organizationID)))
	defer span.End()

	if err := s.service.Warm(ctx, organizationID, orgencryption.UseCaseChatTranscripts); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, string(classify(err)))
		return safe(err)
	}
	return nil
}

// Capture seals each message's transcript from its plaintext row, as shadow
// mode does: an existing transcript is replaced. It uses the caller's
// transaction and never calls KMS: run Prepare before opening it. The caller
// must roll back on error; a batch is not an independently committed unit.
// IDs are deduplicated and sorted.
func (s *Store) Capture(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) error {
	if len(messageIDs) == 0 {
		return nil
	}
	// Reject bad arguments before telemetry so a misused store fails closed
	// with ErrInvalid instead of touching uninitialized tracer or metrics.
	if db == nil || organizationID == "" || projectID == uuid.Nil {
		return ErrInvalid
	}

	ctx, span := s.tracer.Start(ctx, "transcriptstore.Capture", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowMessageCount(len(messageIDs)),
	))
	defer span.End()

	start := time.Now()
	err := s.capture(ctx, db, organizationID, projectID, messageIDs)
	result := classify(err)
	s.metrics.capture.record(ctx, projectID, len(messageIDs), time.Since(start), result)

	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
	}
	return err
}

func (s *Store) capture(ctx context.Context, db repo.DBTX, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) error {
	codec, err := transcriptcrypto.NewIn(s.service, db, organizationID)
	if err != nil {
		return safe(err)
	}
	ids := append([]uuid.UUID(nil), messageIDs...)
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	q := repo.New(db)
	for i, id := range ids {
		if i > 0 && id == ids[i-1] {
			continue
		}
		row, err := lockChatMessage(ctx, q, organizationID, projectID, id)
		if err != nil {
			return err
		}
		if err := seal(ctx, q, codec, organizationID, projectID, row.ChatMessage.ChatID, row.ChatMessage.ID, protectedFieldsOf(row.ChatMessage)); err != nil {
			return err
		}
	}
	return nil
}

// UpdateChatMessage is the shadow-mode message update with a trace span and
// metrics; see Transcripts.UpdateChatMessage.
func (s *Store) UpdateChatMessage(ctx context.Context, db repo.DBTX, organizationID string, projectID, chatID, messageID uuid.UUID, apply func(*chatrepo.ChatMessage) error) (bool, error) {
	return s.updateChatMessageIn(ctx, db, false, organizationID, projectID, chatID, messageID, apply)
}

func (s *Store) updateChatMessageIn(ctx context.Context, db repo.DBTX, encrypted bool, organizationID string, projectID, chatID, messageID uuid.UUID, apply func(*chatrepo.ChatMessage) error) (bool, error) {
	if db == nil || organizationID == "" || projectID == uuid.Nil {
		return false, ErrInvalid
	}
	ctx, span := s.tracer.Start(ctx, "transcriptstore.UpdateChatMessage", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowMessageCount(1),
	))
	defer span.End()

	start := time.Now()
	changed, err := updateChatMessage(ctx, db, s, encrypted, organizationID, projectID, chatID, messageID, apply)
	result := classify(err)
	s.metrics.update.record(ctx, projectID, 1, time.Since(start), result)

	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
	}
	return changed, err
}

// updatableColumns are the chat_messages fields an update may change: the six
// protected fields and the other columns that change with them. apply gets
// the whole row but may change only these.
var updatableColumns = map[string]bool{
	"Content": true, "ContentRaw": true, "ToolCalls": true, "ToolOutcomeNotes": true,
	"ToolCallSummaries": true, "StorageError": true,
	"ToolOutcome": true, "FinishReason": true, "ContentHash": true, "RiskAnalyzedAt": true,
}

// updateMessage is the one read-modify-write of an existing message. It locks
// the row and its transcript, hands apply the whole row with its protected fields filled
// in (from the decrypted transcript, outside plaintext mode), and when apply
// changed anything writes the row, the metadata and, with a store, the
// encrypted transcript, all from that one value in the caller's transaction.
// A nil store is plaintext mode. In shadow mode a missing or disagreeing
// transcript is logged and the plaintext row is used. In encrypted mode the
// transcript is the only copy: it must exist, and the protected fields are written
// empty. The caller runs Prepare before opening the transaction and rolls back
// on error.
func updateChatMessage(ctx context.Context, db repo.DBTX, store *Store, encrypted bool, organizationID string, projectID, chatID, messageID uuid.UUID, apply func(*chatrepo.ChatMessage) error) (bool, error) {
	if db == nil || organizationID == "" || projectID == uuid.Nil || chatID == uuid.Nil || messageID == uuid.Nil || apply == nil {
		return false, ErrInvalid
	}
	q := repo.New(db)
	row, err := lockChatMessage(ctx, q, organizationID, projectID, messageID)
	if err != nil {
		return false, err
	}
	if row.ChatMessage.ChatID != chatID {
		return false, ErrNotFound
	}
	fields := protectedFieldsOf(row.ChatMessage)
	consistent := true
	var codec *transcriptcrypto.Codec
	if store != nil {
		codec, err = transcriptcrypto.NewIn(store.service, db, organizationID)
		if err != nil {
			return false, safe(err)
		}
		fields, consistent, err = store.hydrate(ctx, codec, encrypted, projectID, row)
		if err != nil {
			return false, err
		}
	}
	before := WithProtectedFields(chatrepo.ChatMessage(row.ChatMessage), fields)
	after := cloneRow(before)
	if err := apply(&after); err != nil {
		return false, fmt.Errorf("apply message change: %w", err)
	}
	if field := changedReadOnlyField(before, after); field != "" {
		return false, fmt.Errorf("%w: an update cannot change %s", ErrInvalid, field)
	}
	// In encrypted mode a row still holding plaintext (sealed in shadow mode)
	// is emptied even when apply changes nothing.
	populated := encrypted && !sameProtectedFields(protectedFieldsOf(row.ChatMessage), emptyProtectedFields)
	if sameRow(before, after) && !populated {
		if store != nil && consistent {
			store.metrics.check(ctx, projectID, outcomeSuccess)
		}
		return false, nil
	}
	fields = protectedFieldsOf(repo.ChatMessage(after))
	if encrypted {
		// Seal and measure the JSON in the text form jsonb stores, as a
		// plaintext row would hold it.
		fields, err = canonical(ctx, q, fields)
		if err != nil {
			return false, err
		}
		after = WithProtectedFields(after, emptyProtectedFields)
	}
	written, err := q.WriteChatMessage(ctx, writeParams(projectID, after))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, safe(err)
	}
	// Seal what Postgres stored, so the transcript and the metadata match the
	// plaintext row byte for byte. In encrypted mode the plaintext row is
	// empty, so the transcript holds the protected fields itself.
	sealed := protectedFieldsOf(written)
	if encrypted {
		sealed = fields
	}
	if store == nil {
		return true, writeMetadata(ctx, q, projectID, written.ID, sealed)
	}
	if err := seal(ctx, q, codec, organizationID, projectID, written.ChatID, written.ID, sealed); err != nil {
		return false, err
	}
	return true, store.sanityCheck(ctx, q, codec, organizationID, projectID, written, sealed, consistent)
}

// canonical returns fields with its JSON fields in the text form Postgres stores
// for jsonb.
func canonical(ctx context.Context, q *repo.Queries, fields transcriptcrypto.ProtectedFields) (transcriptcrypto.ProtectedFields, error) {
	row, err := q.CanonicalJSON(ctx, repo.CanonicalJSONParams{ContentRaw: fields.ContentRaw, ToolCalls: fields.ToolCalls, ToolCallSummaries: fields.ToolCallSummaries})
	if err != nil {
		return fields, safe(err)
	}
	orNil := func(v string) []byte {
		if v == "" {
			return nil
		}
		return []byte(v)
	}
	fields.ContentRaw, fields.ToolCalls, fields.ToolCallSummaries = orNil(row.ContentRaw), orNil(row.ToolCalls), orNil(row.ToolCallSummaries)
	return fields, nil
}

// emptyProtectedFields is what the plaintext columns hold in encrypted mode: content is
// NOT NULL, so it is the empty string; every other field is NULL.
var emptyProtectedFields = transcriptcrypto.ProtectedFields{Content: "", ContentRaw: nil, ToolCalls: nil, ToolOutcomeNotes: nil, ToolCallSummaries: nil, StorageError: nil}

// cloneRow copies the row so apply cannot change the original through a shared
// slice.
func cloneRow(m chatrepo.ChatMessage) chatrepo.ChatMessage {
	c := m
	c.ContentRaw = bytes.Clone(m.ContentRaw)
	c.ToolCalls = bytes.Clone(m.ToolCalls)
	c.ToolCallSummaries = bytes.Clone(m.ToolCallSummaries)
	c.ContentHash = bytes.Clone(m.ContentHash)
	return c
}

// changedReadOnlyField names the first field apply changed that an update may
// not write, or returns "".
func changedReadOnlyField(before, after chatrepo.ChatMessage) string {
	b, a := reflect.ValueOf(before), reflect.ValueOf(after)
	for i := range b.NumField() {
		name := b.Type().Field(i).Name
		if updatableColumns[name] {
			continue
		}
		if !reflect.DeepEqual(b.Field(i).Interface(), a.Field(i).Interface()) {
			return name
		}
	}
	return ""
}

// sameRow reports whether apply left every writable field as it was, comparing
// JSON by value.
func sameRow(before, after chatrepo.ChatMessage) bool {
	return sameProtectedFields(protectedFieldsOf(repo.ChatMessage(before)), protectedFieldsOf(repo.ChatMessage(after))) &&
		before.ToolOutcome == after.ToolOutcome && before.FinishReason == after.FinishReason &&
		bytes.Equal(before.ContentHash, after.ContentHash) && before.RiskAnalyzedAt == after.RiskAnalyzedAt
}

func writeParams(projectID uuid.UUID, m chatrepo.ChatMessage) repo.WriteChatMessageParams {
	return repo.WriteChatMessageParams{
		MessageID:         m.ID,
		ProjectID:         project(projectID),
		Content:           m.Content,
		ContentRaw:        m.ContentRaw,
		ToolCalls:         m.ToolCalls,
		ToolOutcomeNotes:  m.ToolOutcomeNotes,
		ToolCallSummaries: m.ToolCallSummaries,
		StorageError:      m.StorageError,
		ToolOutcome:       m.ToolOutcome,
		FinishReason:      m.FinishReason,
		ContentHash:       m.ContentHash,
		RiskAnalyzedAt:    m.RiskAnalyzedAt,
	}
}

// sanityCheck reads back the transcript just written, decrypts it and compares
// it with the stored plaintext row. A difference is logged with each field's
// size and nullness, never its contents, and counted; it does not fail the
// update.
func (s *Store) sanityCheck(ctx context.Context, q *repo.Queries, codec *transcriptcrypto.Codec, organizationID string, projectID uuid.UUID, row repo.ChatMessage, want transcriptcrypto.ProtectedFields, consistent bool) error {
	ciphertext, err := q.CurrentCiphertext(ctx, repo.CurrentCiphertextParams{MessageID: row.ID, ProjectID: projectID, OrganizationID: organizationID})
	if err != nil {
		s.metrics.check(ctx, projectID, outcomeError)
		return safe(err)
	}
	opened, err := codec.Decrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.ChatID, MessageID: row.ID}, ciphertext)
	if err != nil {
		s.metrics.check(ctx, projectID, outcomeError)
		return safe(err)
	}
	if mismatch := describeMismatch(opened, want); len(mismatch) > 0 {
		s.logger.ErrorContext(ctx, "sanity check: stored transcript does not match the protected fields just written",
			attr.SlogProjectID(projectID.String()), attr.SlogMessageID(row.ID.String()), attr.SlogTranscriptShadowMismatch(mismatch))
		s.metrics.check(ctx, projectID, outcomeSanityMismatch)
		return nil
	}
	if consistent {
		s.metrics.check(ctx, projectID, outcomeSuccess)
	}
	return nil
}

// hydrate returns the protected fields an update starts from in shadow mode: the plaintext
// row with its protected fields replaced by the decrypted transcript, locked until
// the transaction ends. In shadow mode the two hold the same protected fields, so this is
// redundant, but it is the path an encrypted-only project takes, so both modes
// run the same code. A message without a transcript (none yet, or only a
// legacy one) or with one that disagrees with the plaintext row starts from
// the plaintext row, which stays the source of truth while both copies exist.
// Both cases are logged as errors and counted; consistent is false for them.
func (s *Store) hydrate(ctx context.Context, codec *transcriptcrypto.Codec, encrypted bool, projectID uuid.UUID, row repo.LockChatMessageRow) (fields transcriptcrypto.ProtectedFields, consistent bool, err error) {
	plain := protectedFieldsOf(row.ChatMessage)
	// LockChatMessage read and locked the current transcript with the row.
	ciphertext := row.CurrentCiphertext
	if encrypted && ciphertext == "" {
		s.metrics.check(ctx, projectID, outcomeMissingTranscript)
		return plain, false, ErrMissingTranscript
	}
	if ciphertext == "" {
		s.logger.ErrorContext(ctx, "message has no current encrypted transcript to update, using the plaintext row",
			attr.SlogProjectID(projectID.String()), attr.SlogMessageID(row.ChatMessage.ID.String()))
		s.metrics.check(ctx, projectID, outcomeMissingTranscript)
		return plain, false, nil
	}
	opened, err := codec.Decrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.ChatMessage.ChatID, MessageID: row.ChatMessage.ID}, ciphertext)
	if err != nil {
		s.metrics.check(ctx, projectID, outcomeError)
		return plain, false, safe(err)
	}
	// In encrypted mode the plaintext columns are empty, so there is nothing
	// to compare with: the transcript is the protected fields.
	if encrypted {
		return opened, true, nil
	}
	if !s.agrees(ctx, "update", projectID, row.ChatMessage.ID, opened, plain) {
		s.metrics.check(ctx, projectID, outcomeHydrateMismatch)
		return plain, false, nil
	}
	return opened, true, nil
}

// fieldMismatch describes one protected field that differs between the plaintext
// row and the transcript, by size and nullness only: logging the values would
// copy the plaintext the transcript protects into the logs.
type fieldMismatch struct {
	Field           string `json:"field"`
	PlaintextBytes  int    `json:"plaintext_bytes"`
	TranscriptBytes int    `json:"transcript_bytes"`
	PlaintextNull   bool   `json:"plaintext_null"`
	TranscriptNull  bool   `json:"transcript_null"`
}

// describeMismatch lists the fields on which the transcript and the plaintext
// row differ, with their sizes and nullness.
func describeMismatch(transcript, plain transcriptcrypto.ProtectedFields) []fieldMismatch {
	var mismatches []fieldMismatch
	for _, field := range differingFields(transcript, plain) {
		m := fieldMismatch{Field: field, PlaintextBytes: 0, TranscriptBytes: 0, PlaintextNull: false, TranscriptNull: false}
		switch field {
		case "content":
			m.PlaintextBytes, m.TranscriptBytes = len(plain.Content), len(transcript.Content)
		case "content_raw":
			m.PlaintextBytes, m.TranscriptBytes = len(plain.ContentRaw), len(transcript.ContentRaw)
			m.PlaintextNull, m.TranscriptNull = plain.ContentRaw == nil, transcript.ContentRaw == nil
		case "tool_calls":
			m.PlaintextBytes, m.TranscriptBytes = len(plain.ToolCalls), len(transcript.ToolCalls)
			m.PlaintextNull, m.TranscriptNull = plain.ToolCalls == nil, transcript.ToolCalls == nil
		case "tool_outcome_notes":
			m.PlaintextBytes, m.PlaintextNull = textSize(plain.ToolOutcomeNotes)
			m.TranscriptBytes, m.TranscriptNull = textSize(transcript.ToolOutcomeNotes)
		case "tool_call_summaries":
			m.PlaintextBytes, m.TranscriptBytes = len(plain.ToolCallSummaries), len(transcript.ToolCallSummaries)
			m.PlaintextNull, m.TranscriptNull = plain.ToolCallSummaries == nil, transcript.ToolCallSummaries == nil
		case "storage_error":
			m.PlaintextBytes, m.PlaintextNull = textSize(plain.StorageError)
			m.TranscriptBytes, m.TranscriptNull = textSize(transcript.StorageError)
		}
		mismatches = append(mismatches, m)
	}
	return mismatches
}

func textSize(v *string) (int, bool) {
	if v == nil {
		return 0, true
	}
	return len(*v), false
}

// differingFields names the protected fields on which two messages differ, with the
// same comparison Load uses. It never returns their contents.
func differingFields(a, b transcriptcrypto.ProtectedFields) []string {
	var fields []string
	if a.Content != b.Content {
		fields = append(fields, "content")
	}
	if !sameJSON(a.ContentRaw, b.ContentRaw) {
		fields = append(fields, "content_raw")
	}
	if !sameJSON(a.ToolCalls, b.ToolCalls) {
		fields = append(fields, "tool_calls")
	}
	if !reflect.DeepEqual(a.ToolOutcomeNotes, b.ToolOutcomeNotes) {
		fields = append(fields, "tool_outcome_notes")
	}
	if !sameJSON(a.ToolCallSummaries, b.ToolCallSummaries) {
		fields = append(fields, "tool_call_summaries")
	}
	if !reflect.DeepEqual(a.StorageError, b.StorageError) {
		fields = append(fields, "storage_error")
	}
	return fields
}

// snapshot locks the message row until the caller's transaction ends and reads
// its protected fields as Postgres stored it.
func lockChatMessage(ctx context.Context, q *repo.Queries, organizationID string, projectID, messageID uuid.UUID) (repo.LockChatMessageRow, error) {
	row, err := q.LockChatMessage(ctx, repo.LockChatMessageParams{MessageID: messageID, ProjectID: project(projectID), OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, safe(err)
	}
	return row, nil
}

func protectedFieldsOf(m repo.ChatMessage) transcriptcrypto.ProtectedFields {
	return message(m.Content, m.ContentRaw, m.ToolCalls, m.ToolOutcomeNotes, m.ToolCallSummaries, m.StorageError)
}

// seal encrypts protected fields as the transcript of the locked message and writes it, with
// the metadata and tool-call IDs measured from those fields. The row stays locked
// until the transaction commits, so the transcript is current from then on.
func seal(ctx context.Context, q *repo.Queries, codec *transcriptcrypto.Codec, organizationID string, projectID, chatID, messageID uuid.UUID, fields transcriptcrypto.ProtectedFields) error {
	ciphertext, err := codec.Encrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: chatID, MessageID: messageID}, fields)
	if err != nil {
		return safe(err)
	}
	n, err := q.Capture(ctx, repo.CaptureParams{
		MessageID:      messageID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
		Ciphertext:     ciphertext,
	})
	if err != nil {
		return safe(err)
	}
	if n != 1 {
		return ErrNotFound
	}
	return writeMetadata(ctx, q, projectID, messageID, fields)
}

// writeMetadata writes the counts and sizes to chat_messages and replaces the
// tool-call IDs, measured from the protected fields just written while the row is locked in
// the same transaction, so they never describe another version of the message.
func writeMetadata(ctx context.Context, q *repo.Queries, projectID, messageID uuid.UUID, fields transcriptcrypto.ProtectedFields) error {
	meta := metadataOf(fields.Content, fields.ContentRaw, fields.ToolCalls)
	n, err := q.UpdateMessageMetadata(ctx, repo.UpdateMessageMetadataParams{
		MessageID:       messageID,
		ProjectID:       projectID,
		ToolCallCount:   meta.toolCallCount,
		ContentBytes:    meta.contentBytes,
		ContentRawBytes: meta.contentRawBytes,
		ToolCallsBytes:  meta.toolCallsBytes,
	})
	if err != nil {
		return safe(err)
	}
	if n != 1 {
		return ErrNotFound
	}
	if err := q.DeleteToolCallIDs(ctx, repo.DeleteToolCallIDsParams{MessageID: messageID, ProjectID: projectID}); err != nil {
		return safe(err)
	}
	if len(meta.toolCallIDs) == 0 {
		return nil
	}
	if err := q.InsertToolCallIDs(ctx, repo.InsertToolCallIDsParams{MessageID: messageID, ProjectID: projectID, ToolCallIds: meta.toolCallIDs}); err != nil {
		return safe(err)
	}
	return nil
}

// Load is the strict single-message check that tests and tools use: it refuses
// missing and legacy transcripts, and stale ones, a transcript that decrypts to
// anything but the current protected fields. Readers use LoadMany, which serves plaintext
// instead.
func (s *Store) Load(ctx context.Context, organizationID string, projectID, messageID uuid.UUID) (transcriptcrypto.ProtectedFields, error) {
	var zero transcriptcrypto.ProtectedFields
	if organizationID == "" || projectID == uuid.Nil || messageID == uuid.Nil {
		return zero, ErrInvalid
	}
	row, err := repo.New(s.db).Load(ctx, repo.LoadParams{MessageID: messageID, ProjectID: project(projectID), OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrNotFound
	}
	if err != nil {
		return zero, safe(err)
	}
	codec, err := transcriptcrypto.New(s.service, organizationID)
	if err != nil {
		return zero, safe(err)
	}
	opened, err := codec.Decrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.ChatID, MessageID: messageID}, row.Ciphertext)
	if err != nil {
		return zero, safe(err)
	}
	source := message(row.Content, row.ContentRaw, row.ToolCalls, row.ToolOutcomeNotes, row.ToolCallSummaries, row.StorageError)
	if !sameProtectedFields(opened, source) {
		return zero, ErrStale
	}
	return opened, nil
}

// LoadMany opens the transcripts of a page of messages with one codec backed by the
// process's committed data key cache: a warm cache needs no KMS call, and a
// cold one unwraps the organization's key once for the page. Only messages
// whose transcript opened and agrees with the plaintext row are in the result;
// the caller serves plaintext for the rest. A transcript that cannot be
// decrypted fails the whole page. Duplicate IDs are opened once.
func (s *Store) LoadMany(ctx context.Context, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) (map[uuid.UUID]transcriptcrypto.ProtectedFields, error) {
	opened, err := s.open(ctx, organizationID, projectID, messageIDs, false)
	if err != nil {
		return nil, err
	}
	served := make(map[uuid.UUID]transcriptcrypto.ProtectedFields, len(opened))
	for id, o := range opened {
		if o.outcome == outcomeSuccess {
			served[id] = o.fields
		}
	}
	return served, nil
}

// opening is what a read serves for one message, and why: the decrypted
// transcript (success), or the plaintext row when the message has no
// transcript (plaintext_fallback) or its transcript disagrees (stale).
type opening struct {
	fields  transcriptcrypto.ProtectedFields
	outcome outcome
}

// open is LoadMany for either mode, with one opening for every requested
// message in the project. In encrypted mode the plaintext row is empty, so a
// transcript is served without comparison, and a message without a current
// transcript fails the page with ErrMissingTranscript.
func (s *Store) open(ctx context.Context, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID, encrypted bool) (map[uuid.UUID]opening, error) {
	if len(messageIDs) == 0 {
		return map[uuid.UUID]opening{}, nil
	}

	ctx, span := s.tracer.Start(ctx, "transcriptstore.LoadMany", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowMessageCount(len(messageIDs)),
	))
	defer span.End()

	start := time.Now()
	opened, err := s.loadMany(ctx, organizationID, projectID, messageIDs, encrypted)
	result := classify(err)
	s.metrics.read.recordDuration(ctx, projectID, time.Since(start), result)

	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		s.metrics.read.recordMessages(ctx, projectID, len(messageIDs), result)
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
		return nil, err
	}

	// Count per requested ID, so a duplicated ID is counted the same way each
	// time. A message outside the project has no opening and counts as a
	// fallback, since the caller keeps its own row.
	counts := make(map[outcome]int, 3)
	for _, id := range messageIDs {
		o, ok := opened[id]
		if !ok {
			o.outcome = outcomePlaintextFallback
		}
		counts[o.outcome]++
	}
	for _, result := range []outcome{outcomeSuccess, outcomePlaintextFallback, outcomeStale} {
		s.metrics.read.recordMessages(ctx, projectID, counts[result], result)
	}
	span.SetAttributes(attr.TranscriptShadowPlaintextFallbackCount(counts[outcomePlaintextFallback]))
	return opened, nil
}

func (s *Store) loadMany(ctx context.Context, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID, encrypted bool) (map[uuid.UUID]opening, error) {
	if organizationID == "" || projectID == uuid.Nil {
		return nil, ErrInvalid
	}
	wanted := make(map[uuid.UUID]struct{}, len(messageIDs))
	for _, id := range messageIDs {
		if id == uuid.Nil {
			return nil, ErrInvalid
		}
		wanted[id] = struct{}{}
	}
	ids := make([]uuid.UUID, 0, len(wanted))
	for id := range wanted {
		ids = append(ids, id)
	}

	rows, err := repo.New(s.db).LoadMany(ctx, repo.LoadManyParams{MessageIds: ids, ProjectID: project(projectID), OrganizationID: organizationID})
	if err != nil {
		return nil, safe(err)
	}
	// In encrypted mode every message must have a current transcript, so a
	// message outside the project fails the page as well.
	if encrypted && len(rows) != len(ids) {
		return nil, ErrMissingTranscript
	}
	codec, err := transcriptcrypto.New(s.service, organizationID)
	if err != nil {
		return nil, safe(err)
	}
	opened := make(map[uuid.UUID]opening, len(rows))
	for _, row := range rows {
		o, err := s.openRow(ctx, codec, projectID, row, encrypted)
		if err != nil {
			return nil, err
		}
		opened[row.ID] = o
	}
	return opened, nil
}

// openRow decides what one message serves. In shadow mode the plaintext row
// is the source of truth while both copies exist.
func (s *Store) openRow(ctx context.Context, codec *transcriptcrypto.Codec, projectID uuid.UUID, row repo.LoadManyRow, encrypted bool) (opening, error) {
	plain := message(row.Content, row.ContentRaw, row.ToolCalls, row.ToolOutcomeNotes, row.ToolCallSummaries, row.StorageError)
	if row.Ciphertext == "" && encrypted {
		return opening{}, ErrMissingTranscript
	}
	if row.Ciphertext == "" {
		return opening{fields: plain, outcome: outcomePlaintextFallback}, nil
	}
	msg, err := codec.Decrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.ChatID, MessageID: row.ID}, row.Ciphertext)
	if err != nil {
		return opening{}, safe(err)
	}
	if !encrypted && !s.agrees(ctx, "read", projectID, row.ID, msg, plain) {
		return opening{fields: plain, outcome: outcomeStale}, nil
	}
	return opening{fields: msg, outcome: outcomeSuccess}, nil
}

// agrees compares a decrypted transcript with its plaintext row, the same way
// on every read and update path. In shadow mode the plaintext row is the
// source of truth: a transcript that disagrees is logged as an error with each
// differing field's size and nullness, never its contents, and the caller uses
// the plaintext row instead.
func (s *Store) agrees(ctx context.Context, path string, projectID, messageID uuid.UUID, transcript, plain transcriptcrypto.ProtectedFields) bool {
	mismatch := describeMismatch(transcript, plain)
	if len(mismatch) == 0 {
		return true
	}
	s.logger.ErrorContext(ctx, "encrypted transcript disagrees with the plaintext row, using the plaintext row",
		attr.SlogProjectID(projectID.String()), attr.SlogMessageID(messageID.String()),
		attr.SlogTranscriptShadowMismatch(mismatch), attr.SlogReason(path))
	return false
}

type Page struct {
	After uuid.UUID
	Count int
	More  bool
}

// Backfill is for shadow-mode projects. It commits a whole bounded page or
// returns no progress. Every message is resealed from its plaintext row, even one with a current transcript: that clears a legacy
// source_hash and repairs a transcript left stale by a write that bypassed the
// application, such as one from a pod that predates resealing. Reset the cursor
// to zero on the next sweep to catch messages inserted below the preceding sweep's cursor.
func (s *Store) Backfill(ctx context.Context, organizationID string, projectID, after uuid.UUID, limit int32) (Page, error) {
	if organizationID == "" || projectID == uuid.Nil || limit < 1 || limit > 500 {
		return Page{}, ErrInvalid
	}
	if err := s.Prepare(ctx, organizationID); err != nil {
		return Page{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Page{}, safe(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ids, err := repo.New(tx).Backfill(ctx, repo.BackfillParams{ProjectID: project(projectID), OrganizationID: organizationID, AfterID: after, PageLimit: limit + 1})
	if err != nil {
		return Page{}, safe(err)
	}
	more := len(ids) > int(limit)
	if more {
		ids = ids[:limit]
	}
	if err = s.Capture(ctx, tx, organizationID, projectID, ids); err != nil {
		return Page{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Page{}, safe(err)
	}
	page := Page{After: after, Count: len(ids), More: more}
	if len(ids) > 0 {
		page.After = ids[len(ids)-1]
	}
	return page, nil
}

// Progress counts messages with a transcript sealed since protected fields updates began
// resealing, not authenticated ciphertext or plaintext removal. Only Load authenticates transcripts. Unattributed rows
// belong to a scoped chat but cannot safely be captured without ownership repair.
type Progress struct{ Current, Waiting, Unattributed int64 }

func (s *Store) Progress(ctx context.Context, organizationID string, projectID uuid.UUID) (Progress, error) {
	if organizationID == "" || projectID == uuid.Nil {
		return Progress{}, ErrInvalid
	}
	row, err := repo.New(s.db).Progress(ctx, repo.ProgressParams{ProjectID: projectID, OrganizationID: organizationID})
	if err != nil {
		return Progress{}, safe(err)
	}
	return Progress{Current: row.Current, Waiting: row.Waiting, Unattributed: row.Unattributed}, nil
}

func sameProtectedFields(a, b transcriptcrypto.ProtectedFields) bool {
	return len(differingFields(a, b)) == 0
}

// SameJSON reports whether two JSON documents hold the same value, ignoring
// formatting and key order, with numbers compared exactly. Both nil is equal;
// nil and non-nil are not. It is how the transcript store compares protected fields, so a
// caller can use it to skip an update that would change nothing.
func SameJSON(a, b []byte) bool { return sameJSON(a, b) }

func sameJSON(a, b []byte) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	decode := func(raw []byte) (any, error) {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		if err := d.Decode(&v); err != nil {
			return nil, safe(err)
		}
		return v, nil
	}
	x, err := decode(a)
	if err != nil {
		return false
	}
	y, err := decode(b)
	return err == nil && sameValue(x, y)
}
func sameValue(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		p, ok := new(big.Rat).SetString(string(x))
		if !ok {
			return false
		}
		q, ok := new(big.Rat).SetString(string(y))
		return ok && p.Cmp(q) == 0
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameValue(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !sameValue(v, w) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

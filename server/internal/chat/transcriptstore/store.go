// Package transcriptstore maintains encrypted shadow copies. Coverage never
// implies plaintext removal; this package never updates message source fields.
package transcriptstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

var (
	ErrNotFound = errors.New("transcript source or shadow not found")
	ErrStale    = errors.New("transcript shadow is stale")
	ErrInvalid  = errors.New("invalid transcript store arguments")
)

type Store struct {
	db      *pgxpool.Pool
	service *orgencryption.Service
	tracer  trace.Tracer
	metrics *storeMetrics
}

func New(db *pgxpool.Pool, service *orgencryption.Service, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) (*Store, error) {
	if db == nil || service == nil || tracerProvider == nil || meterProvider == nil {
		return nil, ErrInvalid
	}
	metrics, err := newStoreMetrics(meterProvider)
	if err != nil {
		return nil, err
	}
	return &Store{
		db:      db,
		service: service,
		tracer:  tracerProvider.Tracer(instrumentationName),
		metrics: metrics,
	}, nil
}

// operationError deliberately hides database/provider details while preserving errors.Is.
type operationError struct{ cause error }

func (e *operationError) Error() string { return "transcript shadow operation failed" }
func (e *operationError) Unwrap() error { return e.cause }
func safe(err error) error {
	if err == nil {
		return nil
	}
	return &operationError{cause: err}
}
func project(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }
func message(content string, raw, calls []byte, notes pgtype.Text, summaries []byte, storageError pgtype.Text) transcriptcrypto.Message {
	m := transcriptcrypto.Message{StorageError: nil, Content: content, ContentRaw: raw, ToolCalls: calls, ToolOutcomeNotes: nil, ToolCallSummaries: summaries}
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

// Capture uses the caller's transaction and never calls KMS: run Prepare
// before opening it. The caller must roll back on error;
// a batch is not an independently committed unit. IDs are deduplicated and sorted.
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
		row, err := snapshot(ctx, q, organizationID, projectID, id)
		if err != nil {
			return err
		}
		if err := seal(ctx, q, codec, organizationID, projectID, row, sourceOf(row)); err != nil {
			return err
		}
	}
	return nil
}

// Update changes the encrypted transcript of one message whose body the
// caller just updated in plaintext, inside the caller's transaction. It
// decrypts the current transcript, applies the same change in Go and
// encrypts the result, so the transcript, not the plaintext, is what the
// change builds on. A message without a transcript, or with only a legacy one,
// starts from its plaintext row. The changed body must equal the updated
// plaintext row, or Update fails with ErrStale and the caller rolls back. Run
// Prepare before opening the transaction.
func (s *Store) Update(ctx context.Context, db repo.DBTX, organizationID string, projectID, messageID uuid.UUID, apply func(*transcriptcrypto.Message) error) error {
	if db == nil || organizationID == "" || projectID == uuid.Nil || messageID == uuid.Nil || apply == nil {
		return ErrInvalid
	}

	ctx, span := s.tracer.Start(ctx, "transcriptstore.Update", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowMessageCount(1),
	))
	defer span.End()

	start := time.Now()
	err := s.update(ctx, db, organizationID, projectID, messageID, apply)
	result := classify(err)
	s.metrics.capture.record(ctx, projectID, 1, time.Since(start), result)

	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
	}
	return err
}

func (s *Store) update(ctx context.Context, db repo.DBTX, organizationID string, projectID, messageID uuid.UUID, apply func(*transcriptcrypto.Message) error) error {
	codec, err := transcriptcrypto.NewIn(s.service, db, organizationID)
	if err != nil {
		return safe(err)
	}
	q := repo.New(db)
	row, err := snapshot(ctx, q, organizationID, projectID, messageID)
	if err != nil {
		return err
	}
	source := sourceOf(row)
	body := source
	identity := transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.ChatID, MessageID: messageID}
	ciphertext, err := q.CurrentCiphertext(ctx, repo.CurrentCiphertextParams{MessageID: messageID, ProjectID: projectID, OrganizationID: organizationID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return safe(err)
	}
	if err == nil {
		body, err = codec.Decrypt(ctx, identity, ciphertext)
		if err != nil {
			return safe(err)
		}
	}
	if err := apply(&body); err != nil {
		return fmt.Errorf("apply transcript change: %w", err)
	}
	// The plaintext row already holds the update. Sealing a body that
	// disagrees with it would leave a transcript every read rejects.
	if !sameMessage(body, source) {
		return ErrStale
	}
	return seal(ctx, q, codec, organizationID, projectID, row, body)
}

// snapshot locks the message row until the caller's transaction ends and reads
// its body as Postgres stored it.
func snapshot(ctx context.Context, q *repo.Queries, organizationID string, projectID, messageID uuid.UUID) (repo.SnapshotRow, error) {
	row, err := q.Snapshot(ctx, repo.SnapshotParams{MessageID: messageID, ProjectID: project(projectID), OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, safe(err)
	}
	return row, nil
}

func sourceOf(row repo.SnapshotRow) transcriptcrypto.Message {
	return message(row.Content, row.ContentRaw, row.ToolCalls, row.ToolOutcomeNotes, row.ToolCallSummaries, row.StorageError)
}

// seal encrypts body as the transcript of the locked row and writes it, with
// the metadata and tool-call IDs measured from the row. The row stays locked
// until the transaction commits, so the transcript is current from then on.
func seal(ctx context.Context, q *repo.Queries, codec *transcriptcrypto.Codec, organizationID string, projectID uuid.UUID, row repo.SnapshotRow, body transcriptcrypto.Message) error {
	ciphertext, err := codec.Encrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.ChatID, MessageID: row.ID}, body)
	if err != nil {
		return safe(err)
	}
	n, err := q.Capture(ctx, repo.CaptureParams{
		MessageID:      row.ID,
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
	// The counts and sizes live on chat_messages and the tool-call IDs in
	// chat_message_tool_call_ids, measured from the same locked row and
	// written in the same transaction, so they never describe another
	// version of the message than the sealed one.
	meta := metadataOf(row.Content, row.ContentRaw, row.ToolCalls)
	n, err = q.UpdateMessageMetadata(ctx, repo.UpdateMessageMetadataParams{
		MessageID:       row.ID,
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
	if err := q.DeleteToolCallIDs(ctx, repo.DeleteToolCallIDsParams{MessageID: row.ID, ProjectID: projectID}); err != nil {
		return safe(err)
	}
	if len(meta.toolCallIDs) == 0 {
		return nil
	}
	if err := q.InsertToolCallIDs(ctx, repo.InsertToolCallIDsParams{MessageID: row.ID, ProjectID: projectID, ToolCallIds: meta.toolCallIDs}); err != nil {
		return safe(err)
	}
	return nil
}

// Load refuses missing and legacy transcripts, and stale ones: a transcript
// that decrypts to anything but the current body, which only a write that
// bypassed the application can leave behind.
func (s *Store) Load(ctx context.Context, organizationID string, projectID, messageID uuid.UUID) (transcriptcrypto.Message, error) {
	var zero transcriptcrypto.Message
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
	if !sameMessage(opened, source) {
		return zero, ErrStale
	}
	return opened, nil
}

// LoadMany opens the shadows of a page of messages with one codec backed by the
// process's committed data key cache: a warm cache needs no KMS call, and a
// cold one unwraps the organization's key once for the page. A message with no
// transcript, or only a legacy one with source_hash, is left out of the result,
// and the caller serves its plaintext.
// Every shadow that does exist gets Load's checks, and one that is stale or not
// authentic fails the whole page: an existing shadow is never skipped in favour
// of plaintext. Duplicate IDs are opened once.
func (s *Store) LoadMany(ctx context.Context, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) (map[uuid.UUID]transcriptcrypto.Message, error) {
	if len(messageIDs) == 0 {
		return map[uuid.UUID]transcriptcrypto.Message{}, nil
	}

	ctx, span := s.tracer.Start(ctx, "transcriptstore.LoadMany", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.OrganizationID(organizationID),
		attr.TranscriptShadowMessageCount(len(messageIDs)),
	))
	defer span.End()

	start := time.Now()
	opened, err := s.loadMany(ctx, organizationID, projectID, messageIDs)
	result := classify(err)
	s.metrics.read.recordDuration(ctx, projectID, time.Since(start), result)

	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		s.metrics.read.recordMessages(ctx, projectID, len(messageIDs), result)
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
		return nil, err
	}

	missing := 0
	for _, id := range messageIDs {
		if _, ok := opened[id]; !ok {
			missing++
		}
	}
	s.metrics.read.recordMessages(ctx, projectID, len(messageIDs)-missing, outcomeSuccess)
	s.metrics.read.recordMessages(ctx, projectID, missing, outcomePlaintextFallback)
	span.SetAttributes(attr.TranscriptShadowPlaintextFallbackCount(missing))
	return opened, nil
}

func (s *Store) loadMany(ctx context.Context, organizationID string, projectID uuid.UUID, messageIDs []uuid.UUID) (map[uuid.UUID]transcriptcrypto.Message, error) {
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
	if len(rows) == 0 {
		return map[uuid.UUID]transcriptcrypto.Message{}, nil
	}

	codec, err := transcriptcrypto.New(s.service, organizationID)
	if err != nil {
		return nil, safe(err)
	}
	opened := make(map[uuid.UUID]transcriptcrypto.Message, len(rows))
	for _, row := range rows {
		msg, err := codec.Decrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.ChatID, MessageID: row.ID}, row.Ciphertext)
		if err != nil {
			return nil, safe(err)
		}
		source := message(row.Content, row.ContentRaw, row.ToolCalls, row.ToolOutcomeNotes, row.ToolCallSummaries, row.StorageError)
		if !sameMessage(msg, source) {
			return nil, ErrStale
		}
		opened[row.ID] = msg
	}
	return opened, nil
}

type Page struct {
	After uuid.UUID
	Count int
	More  bool
}

// Backfill commits a whole bounded page or returns no progress. Every message is
// resealed, even one with a current transcript: that clears a legacy
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

// Progress counts messages with a transcript sealed since body updates began
// resealing, not authenticated ciphertext or plaintext removal. Only Load authenticates shadows. Unattributed rows
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

func sameMessage(a, b transcriptcrypto.Message) bool {
	return reflect.DeepEqual(a.StorageError, b.StorageError) && a.Content == b.Content && reflect.DeepEqual(a.ToolOutcomeNotes, b.ToolOutcomeNotes) && sameJSON(a.ContentRaw, b.ContentRaw) && sameJSON(a.ToolCalls, b.ToolCalls) && sameJSON(a.ToolCallSummaries, b.ToolCallSummaries)
}
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

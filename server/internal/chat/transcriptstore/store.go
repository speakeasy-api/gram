// Package transcriptstore maintains encrypted shadow copies. Coverage never
// implies plaintext removal; this package never updates message source fields.
package transcriptstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	ErrChanged  = errors.New("transcript source changed during capture")
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

// Capture uses the caller's transaction. The caller must roll back on error;
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
		row, err := q.Snapshot(ctx, repo.SnapshotParams{MessageID: id, ProjectID: project(projectID), OrganizationID: organizationID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return safe(err)
		}
		ciphertext, err := codec.Encrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.ChatID, MessageID: id}, message(row.Content, row.ContentRaw, row.ToolCalls, row.ToolOutcomeNotes, row.ToolCallSummaries, row.StorageError))
		if err != nil {
			return safe(err)
		}
		meta := metadataOf(row.Content, row.ContentRaw, row.ToolCalls)
		n, err := q.Capture(ctx, repo.CaptureParams{
			MessageID:       id,
			ProjectID:       projectID,
			OrganizationID:  organizationID,
			ChatID:          row.ChatID,
			SourceHash:      row.SourceHash,
			Ciphertext:      ciphertext,
			ToolCallCount:   meta.toolCallCount,
			ContentBytes:    meta.contentBytes,
			ContentRawBytes: meta.contentRawBytes,
			ToolCallsBytes:  meta.toolCallsBytes,
		})
		if err != nil {
			return safe(err)
		}
		if n != 1 {
			return ErrChanged
		}
		// Tool-call IDs are rewritten with the shadow, inside the same
		// transaction, so they never describe an older version of the message.
		if err := q.DeleteToolCallIDs(ctx, id); err != nil {
			return safe(err)
		}
		if len(meta.toolCallIDs) == 0 {
			continue
		}
		if err := q.InsertToolCallIDs(ctx, repo.InsertToolCallIDsParams{MessageID: id, ChatID: row.ChatID, ProjectID: projectID, ToolCallIds: meta.toolCallIDs}); err != nil {
			return safe(err)
		}
	}
	return nil
}

// Load refuses missing/stale shadows, including authenticated older payloads
// whose unencrypted source_hash was forged to match the current source.
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
	if !row.Current {
		return zero, ErrStale
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
// shadow at all is left out of the result, and the caller serves its plaintext.
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
		if !row.Current {
			return nil, ErrStale
		}
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

// Backfill commits a whole bounded page or returns no progress. Retry ErrChanged
// with the same cursor. Every source row is recaptured, even when its public
// fingerprint matches: hashes cannot authenticate a shadow. Reset the cursor
// to zero on the next sweep to catch messages inserted below the preceding sweep's cursor.
func (s *Store) Backfill(ctx context.Context, organizationID string, projectID, after uuid.UUID, limit int32) (Page, error) {
	if organizationID == "" || projectID == uuid.Nil || limit < 1 || limit > 500 {
		return Page{}, ErrInvalid
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

// Progress describes fingerprint-matching coverage, not authenticated ciphertext
// or plaintext removal. Only Load authenticates shadows. Unattributed rows
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

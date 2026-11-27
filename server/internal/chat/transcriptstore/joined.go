package transcriptstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// joinedRow is one message loaded together with its shadow by the same query.
// Every query in internal/plainrepo that reads plaintext body fields LEFT
// JOINs chat_message_transcripts when the project keeps shadows, so the
// ciphertext arrives in the same round trip as the row. The organization
// lookup that opening needs runs once per project per process, on the first
// shadow read, and is cached after that.
type joinedRow struct {
	body       Body
	chatID     uuid.UUID
	ciphertext pgtype.Text
	current    bool

	// shape repeats in Go what the query does to a body field in SQL, such as
	// truncating it or blanking it over a size cap, so the opened shadow is
	// served in the same shape and compares equal to the plaintext column.
	shape func(*transcriptcrypto.Message)
}

// openJoined replaces each row's body with its opened shadow when the project
// keeps shadows. The rules match LoadMany: no shadow at all keeps plaintext and
// counts as plaintext_fallback; in shadow mode a shadow that disagrees with its
// plaintext row is logged, counted as stale, and the plaintext is served; a
// shadow that fails authentication fails the read. In encrypted mode the
// shadow is served without comparison, and a missing one fails the read.
func (t *Transcripts) openJoined(ctx context.Context, db plainrepo.DBTX, projectID uuid.UUID, reader string, rows []joinedRow) error {
	if len(rows) == 0 {
		return nil
	}
	if err := t.checkOpenable(ctx, projectID, reader); err != nil {
		return err
	}
	if !t.Enabled(projectID) {
		return nil
	}
	s := t.store
	ctx, span := s.tracer.Start(ctx, "transcriptstore.OpenJoined", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.TranscriptShadowMessageCount(len(rows)),
	))
	defer span.End()

	start := time.Now()
	missing, stale, err := t.openJoinedRows(ctx, db, projectID, rows, t.Mode(projectID) == ModeEncrypted)
	result := classify(err)
	s.metrics.read.recordDuration(ctx, projectID, time.Since(start), result)
	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		s.metrics.read.recordMessages(ctx, projectID, len(rows), result)
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	s.metrics.read.recordMessages(ctx, projectID, len(rows)-missing-stale, outcomeSuccess)
	s.metrics.read.recordMessages(ctx, projectID, missing, outcomePlaintextFallback)
	s.metrics.read.recordMessages(ctx, projectID, stale, outcomeStale)
	span.SetAttributes(attr.TranscriptShadowPlaintextFallbackCount(missing))
	return nil
}

// openJoinedRows opens the rows' shadows. It reports how many rows had no
// current shadow and how many had one that disagreed with the plaintext row.
func (t *Transcripts) openJoinedRows(ctx context.Context, db plainrepo.DBTX, projectID uuid.UUID, rows []joinedRow, encrypted bool) (int, int, error) {
	missing := 0
	for _, row := range rows {
		if !row.ciphertext.Valid || !row.current {
			missing++
		}
	}
	if encrypted && missing > 0 {
		return 0, 0, ErrMissingTranscript
	}
	if missing == len(rows) {
		return missing, 0, nil
	}
	organizationID, err := t.organization(ctx, db, projectID)
	if err != nil {
		return 0, 0, err
	}
	codec, err := transcriptcrypto.New(t.store.service, organizationID)
	if err != nil {
		return 0, 0, safe(err)
	}
	stale := 0
	for _, row := range rows {
		if !row.ciphertext.Valid || !row.current {
			continue
		}
		message, err := codec.Decrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.chatID, MessageID: row.body.MessageID}, row.ciphertext.String)
		if err != nil {
			return 0, 0, safe(err)
		}
		if row.shape != nil {
			row.shape(&message)
		}
		if !encrypted && !t.store.agrees(ctx, "read", projectID, row.body.MessageID, message, row.body.plain(message)) {
			stale++
			continue
		}
		row.body.assign(message)
	}
	return missing, stale, nil
}

// organization resolves a listed project's organization once per process.
func (t *Transcripts) organization(ctx context.Context, db plainrepo.DBTX, projectID uuid.UUID) (string, error) {
	if cached, ok := t.organizations.Load(projectID); ok {
		if org, ok := cached.(string); ok {
			return org, nil
		}
	}
	org, err := plainrepo.New(db).GetProjectOrganizationID(ctx, projectID)
	if err != nil {
		return "", safe(err)
	}
	if org == "" {
		return "", ErrInvalid
	}
	t.organizations.Store(projectID, org)
	return org, nil
}

// plain returns the opened message with every loaded field replaced by the
// plaintext row's value, so comparing the two compares the loaded fields only.
func (b Body) plain(m transcriptcrypto.Message) transcriptcrypto.Message {
	if b.Content != nil {
		m.Content = *b.Content
	}
	if b.ContentRaw != nil {
		m.ContentRaw = *b.ContentRaw
	}
	if b.ToolCalls != nil {
		m.ToolCalls = *b.ToolCalls
	}
	if b.ToolCallSummaries != nil {
		m.ToolCallSummaries = *b.ToolCallSummaries
	}
	if b.ToolOutcomeNotes != nil {
		m.ToolOutcomeNotes = textPointer(*b.ToolOutcomeNotes)
	}
	if b.StorageError != nil {
		m.StorageError = textPointer(*b.StorageError)
	}
	return m
}

func textPointer(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

// assign writes the opened message into every loaded field.
func (b Body) assign(m transcriptcrypto.Message) {
	if b.Content != nil {
		*b.Content = m.Content
	}
	if b.ContentRaw != nil {
		*b.ContentRaw = m.ContentRaw
	}
	if b.ToolCalls != nil {
		*b.ToolCalls = m.ToolCalls
	}
	if b.ToolCallSummaries != nil {
		*b.ToolCallSummaries = m.ToolCallSummaries
	}
	if b.ToolOutcomeNotes != nil {
		*b.ToolOutcomeNotes = optionalText(m.ToolOutcomeNotes)
	}
	if b.StorageError != nil {
		*b.StorageError = optionalText(m.StorageError)
	}
}

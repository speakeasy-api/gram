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
// ciphertext arrives in the same round trip and no second query runs.
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
// keeps shadows. The rules match Open: no shadow at all keeps plaintext and
// counts as plaintext_fallback; a stale or unauthentic shadow fails the read.
func (t *Transcripts) openJoined(ctx context.Context, db plainrepo.DBTX, projectID uuid.UUID, rows []joinedRow) error {
	if len(rows) == 0 || !t.Enabled(projectID) {
		return nil
	}
	s := t.store
	ctx, span := s.tracer.Start(ctx, "transcriptstore.OpenJoined", trace.WithAttributes(
		attr.ProjectID(projectID.String()),
		attr.TranscriptShadowMessageCount(len(rows)),
	))
	defer span.End()

	start := time.Now()
	missing, err := t.openJoinedRows(ctx, db, projectID, rows)
	result := classify(err)
	s.metrics.read.recordDuration(ctx, projectID, time.Since(start), result)
	span.SetAttributes(attr.Outcome(result))
	if err != nil {
		s.metrics.read.recordMessages(ctx, projectID, len(rows), result)
		span.RecordError(err)
		span.SetStatus(codes.Error, string(result))
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	s.metrics.read.recordMessages(ctx, projectID, len(rows)-missing, outcomeSuccess)
	s.metrics.read.recordMessages(ctx, projectID, missing, outcomePlaintextFallback)
	span.SetAttributes(attr.TranscriptShadowPlaintextFallbackCount(missing))
	return nil
}

func (t *Transcripts) openJoinedRows(ctx context.Context, db plainrepo.DBTX, projectID uuid.UUID, rows []joinedRow) (int, error) {
	missing := 0
	for _, row := range rows {
		if !row.ciphertext.Valid {
			missing++
		}
	}
	if missing == len(rows) {
		return missing, nil
	}
	organizationID, err := t.organization(ctx, db, projectID)
	if err != nil {
		return 0, err
	}
	codec, err := transcriptcrypto.New(t.store.service, organizationID)
	if err != nil {
		return 0, safe(err)
	}
	for _, row := range rows {
		if !row.ciphertext.Valid {
			continue
		}
		if !row.current {
			return 0, ErrStale
		}
		message, err := codec.Decrypt(ctx, transcriptcrypto.Identity{ProjectID: projectID, ChatID: row.chatID, MessageID: row.body.MessageID}, row.ciphertext.String)
		if err != nil {
			return 0, safe(err)
		}
		if row.shape != nil {
			row.shape(&message)
		}
		// The source hash already matched in SQL. Comparing the served fields
		// too refuses an older authentic payload paired with a forged hash.
		if !row.body.matches(message) {
			return 0, ErrStale
		}
		row.body.assign(message)
	}
	return missing, nil
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

// matches reports whether every loaded field equals the opened message.
func (b Body) matches(m transcriptcrypto.Message) bool {
	if b.Content != nil && *b.Content != m.Content {
		return false
	}
	if b.ContentRaw != nil && !sameJSON(*b.ContentRaw, m.ContentRaw) {
		return false
	}
	if b.ToolCalls != nil && !sameJSON(*b.ToolCalls, m.ToolCalls) {
		return false
	}
	if b.ToolCallSummaries != nil && !sameJSON(*b.ToolCallSummaries, m.ToolCallSummaries) {
		return false
	}
	if b.ToolOutcomeNotes != nil && *b.ToolOutcomeNotes != optionalText(m.ToolOutcomeNotes) {
		return false
	}
	if b.StorageError != nil && *b.StorageError != optionalText(m.StorageError) {
		return false
	}
	return true
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

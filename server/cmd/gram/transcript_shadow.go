package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// configureTranscriptShadow wires encrypted transcript shadow capture. The
// chat-transcript-shadow-capture PostHog flag picks projects on each write, so
// startup only checks that capture can work. Any problem is logged and leaves
// capture off: this optional feature never stops a process from starting.
// Plaintext remains authoritative; startup never backfills old messages.
func configureTranscriptShadow(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, writer *chat.ChatMessageWriter, orgEncryption orgEncryptionDeps, flags feature.Provider, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) bool {
	if db == nil || writer == nil || flags == nil || orgEncryption.Service == nil || orgEncryption.Provisioner == nil {
		return false
	}
	if err := checkTranscriptShadowSchema(ctx, db); err != nil {
		logger.ErrorContext(ctx, "chat transcript shadow capture is off", attr.SlogError(err))
		return false
	}
	store, err := transcriptstore.New(db, orgEncryption.Service, tracerProvider, meterProvider)
	if err != nil {
		logger.ErrorContext(ctx, "chat transcript shadow capture is off: create store", attr.SlogError(err))
		return false
	}
	writer.WithTranscriptShadow(store, transcriptstore.NewCaptureGate(logger, flags, orgEncryption.Provisioner))
	return true
}

// checkTranscriptShadowSchema checks only the sidecar contract used by the store.
// Catalog reads neither touch transcript payloads nor execute trial inserts.
// The table is looked up through the connection's search_path, the same way
// the store's queries resolve it: deployed databases keep it outside public.
// Every omitted NOT NULL column must be supplied by the database. All inferred
// arbiters must be immediate: PostgreSQL rejects even a redundant deferred one.
func checkTranscriptShadowSchema(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) error {
	var compatible bool
	err := db.QueryRow(ctx, `
WITH sidecar AS (
 SELECT oid FROM pg_catalog.pg_class
 WHERE oid = pg_catalog.to_regclass('chat_message_transcripts')
 AND relkind IN ('r', 'p')
), required(name, type_oid) AS (
 VALUES ('message_id', 'uuid'::regtype),
        ('project_id', 'uuid'::regtype),
        ('organization_id', 'text'::regtype),
        ('ciphertext', 'text'::regtype),
        ('source_hash', 'bytea'::regtype),
        ('updated_at', 'timestamptz'::regtype)
), arbiters AS (
 SELECT i.indimmediate, i.indisready
 FROM pg_catalog.pg_index i
 JOIN sidecar s ON s.oid = i.indrelid
 JOIN pg_catalog.pg_attribute a ON a.attrelid = s.oid AND a.attname = 'message_id'
 WHERE i.indisunique AND i.indisvalid
   AND i.indpred IS NULL AND i.indexprs IS NULL
   AND i.indnkeyatts = 1 AND i.indkey[0] = a.attnum
)
SELECT EXISTS (SELECT 1 FROM sidecar)
 AND NOT EXISTS (
 SELECT 1 FROM required r
 WHERE NOT EXISTS (
  SELECT 1 FROM pg_catalog.pg_attribute a
  JOIN sidecar s ON s.oid = a.attrelid
  WHERE a.attname = r.name AND a.atttypid = r.type_oid
    AND a.attnotnull AND NOT a.attisdropped
 ))
 AND NOT EXISTS (
 SELECT 1 FROM pg_catalog.pg_attribute a
 JOIN sidecar s ON s.oid = a.attrelid
 WHERE a.attnum > 0 AND NOT a.attisdropped AND a.attnotnull
   AND NOT a.atthasdef AND a.attgenerated = '' AND a.attidentity = ''
   AND a.attname NOT IN ('message_id', 'project_id', 'organization_id', 'ciphertext', 'source_hash')
 )
 AND EXISTS (SELECT 1 FROM arbiters WHERE indimmediate AND indisready)
 AND NOT EXISTS (SELECT 1 FROM arbiters WHERE NOT indimmediate)`).Scan(&compatible)
	if err != nil {
		return fmt.Errorf("check chat transcript shadow schema: %w", err)
	}
	if !compatible {
		return fmt.Errorf("chat transcript shadow schema is unavailable or incompatible")
	}
	return nil
}

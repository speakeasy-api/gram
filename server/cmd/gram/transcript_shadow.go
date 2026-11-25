package gram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// parseTranscriptShadowProjectIDs preserves an explicit, nonempty allowlist.
// Errors deliberately omit the supplied values.
func parseTranscriptShadowProjectIDs(values []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(values))
	seen := make(map[uuid.UUID]struct{}, len(values))
	for i, value := range values {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || id == uuid.Nil {
			return nil, fmt.Errorf("invalid chat transcript shadow project UUID at position %d", i+1)
		}
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
			seen[id] = struct{}{}
		}
	}
	return ids, nil
}

// configureTranscriptShadow only opts explicit projects into encrypted shadow
// writes and reads, and wires the writer to capture for them. Plaintext remains
// authoritative for every other project; startup never backfills old
// messages. It returns nil when no project keeps shadows.
func configureTranscriptShadow(ctx context.Context, c *cli.Context, db *pgxpool.Pool, writer *chat.ChatMessageWriter, orgEncryption orgEncryptionDeps, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) (*transcriptstore.Transcripts, error) {
	var none *transcriptstore.Transcripts
	values := c.StringSlice(chatTranscriptShadowProjectIDsFlag)
	if len(values) == 0 {
		// An explicitly supplied blank must not silently disable the feature.
		if c.IsSet(chatTranscriptShadowProjectIDsFlag) {
			return none, fmt.Errorf("chat transcript shadow requires explicit nonempty project UUIDs")
		}
		return none, nil
	}
	projectIDs, err := parseTranscriptShadowProjectIDs(values)
	if err != nil {
		return none, err
	}
	if orgEncryption.Service == nil || orgEncryption.Provisioner == nil {
		return none, fmt.Errorf("chat transcript shadow requires organization encryption service and provisioner")
	}
	if db == nil || writer == nil {
		return none, fmt.Errorf("chat transcript shadow requires database and writer")
	}
	// Check compatibility before resolving projects, provisioning keys or enabling writes.
	if err := checkTranscriptShadowSchema(ctx, db); err != nil {
		return none, err
	}
	queries := chatrepo.New(db)
	organizations := make([]string, 0, len(projectIDs))
	seen := make(map[string]struct{}, len(projectIDs))
	// Resolve every target before provisioning even the first organization's keys.
	for _, projectID := range projectIDs {
		org, err := queries.GetProjectOrganizationID(ctx, projectID)
		if err != nil {
			return none, fmt.Errorf("resolve chat transcript shadow project: %w", err)
		}
		if strings.TrimSpace(org) == "" {
			return none, fmt.Errorf("chat transcript shadow project has no organization")
		}
		if _, ok := seen[org]; !ok {
			organizations = append(organizations, org)
			seen[org] = struct{}{}
		}
	}
	// Build the store first: it can fail, and provisioning mints keys.
	store, err := transcriptstore.New(db, orgEncryption.Service, tracerProvider, meterProvider)
	if err != nil {
		return none, fmt.Errorf("create chat transcript shadow store: %w", err)
	}
	for _, org := range organizations {
		if err := orgEncryption.Provisioner.EnsureUseCase(ctx, org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
			return none, fmt.Errorf("provision chat transcript shadow encryption: %w", err)
		}
	}
	transcripts := transcriptstore.NewTranscripts(store, projectIDs...)
	writer.WithTranscripts(transcripts)
	return transcripts, nil
}

// configureReadOnlyTranscripts is for a process that reads chat messages but
// does not configure the shadow store. It knows which projects keep shadows,
// so a read for one of them is reported (error log and
// chat.transcript_shadow.plaintext_reads) instead of silently serving
// plaintext, and fails outright in local development. It returns nil when no
// project keeps shadows.
func configureReadOnlyTranscripts(c *cli.Context, logger *slog.Logger, meterProvider metric.MeterProvider) (*transcriptstore.Transcripts, error) {
	values := c.StringSlice(chatTranscriptShadowProjectIDsFlag)
	if len(values) == 0 {
		// An explicitly supplied blank must not silently disable reporting.
		if c.IsSet(chatTranscriptShadowProjectIDsFlag) {
			return nil, fmt.Errorf("chat transcript shadow requires explicit nonempty project UUIDs")
		}
		return nil, nil
	}
	projectIDs, err := parseTranscriptShadowProjectIDs(values)
	if err != nil {
		return nil, err
	}
	strict := c.String("environment") == "local"
	transcripts, err := transcriptstore.NewReadOnlyTranscripts(projectIDs...).WithPlaintextGuard(logger, meterProvider, strict)
	if err != nil {
		return nil, fmt.Errorf("configure read-only chat transcripts: %w", err)
	}
	return transcripts, nil
}

// checkTranscriptShadowSchema checks the tables the store writes: the sidecar
// and its tool-call ID table, including the metadata columns capture fills.
// Catalog reads neither touch transcript payloads nor execute trial inserts.
// The table is looked up through the connection's search_path, the same way
// the store's queries resolve it: deployed databases keep it outside public.
// Every omitted NOT NULL column must be supplied by the database. All inferred
// arbiters must be immediate: PostgreSQL rejects even a redundant deferred one.
// The metadata columns capture writes, and the tool-call ID table beside the
// sidecar, must exist too.
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
 AND NOT EXISTS (SELECT 1 FROM arbiters WHERE NOT indimmediate)
 AND NOT EXISTS (
 SELECT 1 FROM (VALUES ('tool_call_count'), ('content_bytes'), ('content_raw_bytes'), ('tool_calls_bytes')) AS metadata(name)
 WHERE NOT EXISTS (
  SELECT 1 FROM pg_catalog.pg_attribute a
  JOIN sidecar s ON s.oid = a.attrelid
  WHERE a.attname = metadata.name AND a.atttypid = 'integer'::regtype AND NOT a.attisdropped
 ))
 AND NOT EXISTS (
 SELECT 1 FROM (VALUES ('message_id', 'uuid'::regtype), ('tool_call_id', 'text'::regtype), ('chat_id', 'uuid'::regtype), ('project_id', 'uuid'::regtype)) AS tool_call(name, type_oid)
 WHERE NOT EXISTS (
  SELECT 1 FROM pg_catalog.pg_attribute a
  JOIN pg_catalog.pg_class r ON r.oid = a.attrelid AND r.relkind IN ('r', 'p')
  WHERE a.attrelid = pg_catalog.to_regclass('chat_message_transcript_tool_calls')
    AND a.attname = tool_call.name AND a.atttypid = tool_call.type_oid
    AND a.attnotnull AND NOT a.attisdropped
 ))`).Scan(&compatible)
	if err != nil {
		return fmt.Errorf("check chat transcript shadow schema: %w", err)
	}
	if !compatible {
		return fmt.Errorf("chat transcript shadow schema is unavailable or incompatible")
	}
	return nil
}

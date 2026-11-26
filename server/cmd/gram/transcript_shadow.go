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

// configureTranscripts only opts explicit projects into encrypted transcripts,
// and wires the writer to seal them. Plaintext remains authoritative for every
// other project; startup never backfills old messages. It returns nil when no
// project has encrypted transcripts.
func configureTranscripts(ctx context.Context, c *cli.Context, db *pgxpool.Pool, writer *chat.ChatMessageWriter, orgEncryption orgEncryptionDeps, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) (*transcriptstore.Transcripts, error) {
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
	if orgEncryption.Service == nil || (writer != nil && orgEncryption.Provisioner == nil) {
		return none, fmt.Errorf("chat transcript shadow requires organization encryption service and provisioner")
	}
	if db == nil {
		return none, fmt.Errorf("chat transcript shadow requires a database")
	}
	// Check compatibility before resolving projects, provisioning keys or enabling writes.
	if err := checkTranscriptSchema(ctx, db); err != nil {
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
	// Only a process that captures shadows provisions keys. A reader opens
	// shadows that a capturing process already sealed, so it never mints a
	// config or data key.
	for _, org := range organizations {
		if writer == nil {
			break
		}
		if err := orgEncryption.Provisioner.EnsureUseCase(ctx, org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
			return none, fmt.Errorf("provision chat transcript shadow encryption: %w", err)
		}
	}
	transcripts := transcriptstore.NewTranscripts(store, projectIDs...)
	// A process that only reads chat messages has no writer to wire.
	if writer != nil {
		writer.WithTranscripts(transcripts)
	}
	return transcripts, nil
}

// configureReaderTranscripts is for a process that reads chat messages but
// never writes them (streams). With organization encryption it opens shadows
// like any other reader, without provisioning keys. Without it, it falls back to read-only Transcripts,
// which report every read for a listed project.
func configureReaderTranscripts(ctx context.Context, c *cli.Context, db *pgxpool.Pool, orgEncryption orgEncryptionDeps, logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) (*transcriptstore.Transcripts, error) {
	if orgEncryption.Service == nil {
		return configureReadOnlyTranscripts(c, logger, meterProvider)
	}
	return configureTranscripts(ctx, c, db, nil, orgEncryption, tracerProvider, meterProvider)
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

// checkTranscriptSchema checks what the store reads and writes: the sidecar
// and the source_updated_at it records, the chat_messages columns and the
// source_updated_at default that sealing relies on, and the tool-call ID table with the
// (message_id, tool_call_id) conflict target its inserts use.
// Catalog reads neither touch transcript payloads nor execute trial inserts.
// Tables are looked up through the connection's search_path, the same way the
// store's queries resolve them: deployed databases keep them outside public.
// Every omitted NOT NULL column must be supplied by the database. All inferred
// arbiters must be immediate: PostgreSQL rejects even a redundant deferred one.
func checkTranscriptSchema(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) error {
	var compatible bool
	err := db.QueryRow(ctx, `
WITH sidecar AS (
 SELECT oid FROM pg_catalog.pg_class
 WHERE oid = pg_catalog.to_regclass('chat_message_transcripts')
 AND relkind IN ('r', 'p')
), messages AS (
 SELECT oid FROM pg_catalog.pg_class
 WHERE oid = pg_catalog.to_regclass('chat_messages')
 AND relkind IN ('r', 'p')
), tool_calls AS (
 SELECT oid FROM pg_catalog.pg_class
 WHERE oid = pg_catalog.to_regclass('chat_message_tool_call_ids')
 AND relkind IN ('r', 'p')
), required(name, type_oid) AS (
 VALUES ('message_id', 'uuid'::regtype),
        ('project_id', 'uuid'::regtype),
        ('organization_id', 'text'::regtype),
        ('ciphertext', 'text'::regtype),
        ('updated_at', 'timestamptz'::regtype)
), tool_call_arbiters AS (
 -- InsertToolCallIDs resolves conflicts on (message_id, tool_call_id).
 SELECT i.indimmediate, i.indisready
 FROM pg_catalog.pg_index i
 JOIN tool_calls tc ON tc.oid = i.indrelid
 WHERE i.indisunique AND i.indisvalid
   AND i.indpred IS NULL AND i.indexprs IS NULL AND i.indnkeyatts = 2
   AND (SELECT array_agg(a.attname::text ORDER BY a.attname::text)
        FROM pg_catalog.pg_attribute a
        WHERE a.attrelid = i.indrelid AND a.attnum = ANY ((i.indkey::int2[])[0:1])) = ARRAY['message_id', 'tool_call_id']
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
 -- Capture supplies only these columns; any other NOT NULL column needs a
 -- database default.
 SELECT 1 FROM pg_catalog.pg_attribute a
 JOIN sidecar s ON s.oid = a.attrelid
 WHERE a.attnum > 0 AND NOT a.attisdropped AND a.attnotnull
   AND NOT a.atthasdef AND a.attgenerated = '' AND a.attidentity = ''
   AND a.attname NOT IN ('message_id', 'project_id', 'organization_id', 'ciphertext', 'source_updated_at')
 )
 AND EXISTS (SELECT 1 FROM arbiters WHERE indimmediate AND indisready)
 AND NOT EXISTS (SELECT 1 FROM arbiters WHERE NOT indimmediate)
 AND EXISTS (
 SELECT 1 FROM pg_catalog.pg_attribute a
 JOIN sidecar s ON s.oid = a.attrelid
 WHERE a.attname = 'source_updated_at' AND a.atttypid = 'timestamptz'::regtype AND NOT a.attisdropped
 )
 AND NOT EXISTS (
 -- Sealing writes the metadata to chat_messages and compares source_updated_at.
 SELECT 1 FROM (VALUES ('tool_call_count', 'integer'::regtype), ('content_bytes', 'integer'::regtype),
   ('content_raw_bytes', 'integer'::regtype), ('tool_calls_bytes', 'integer'::regtype),
   ('source_updated_at', 'timestamptz'::regtype)) AS column_needed(name, type_oid)
 WHERE NOT EXISTS (
  SELECT 1 FROM pg_catalog.pg_attribute a
  JOIN messages m ON m.oid = a.attrelid
  WHERE a.attname = column_needed.name AND a.atttypid = column_needed.type_oid AND NOT a.attisdropped
 ))
 AND EXISTS (
 -- Inserts rely on the column default to stamp source_updated_at.
 SELECT 1 FROM pg_catalog.pg_attribute a
 JOIN messages m ON m.oid = a.attrelid
 WHERE a.attname = 'source_updated_at' AND a.atthasdef AND NOT a.attisdropped
 )
 AND NOT EXISTS (
 SELECT 1 FROM (VALUES ('message_id', 'uuid'::regtype), ('tool_call_id', 'text'::regtype), ('chat_id', 'uuid'::regtype), ('project_id', 'uuid'::regtype)) AS tool_call(name, type_oid)
 WHERE NOT EXISTS (
  SELECT 1 FROM pg_catalog.pg_attribute a
  JOIN tool_calls tc ON tc.oid = a.attrelid
  WHERE a.attname = tool_call.name AND a.atttypid = tool_call.type_oid
    AND a.attnotnull AND NOT a.attisdropped
 ))
 AND NOT EXISTS (
 -- InsertToolCallIDs supplies only these four columns; any other NOT NULL
 -- column needs a database default.
 SELECT 1 FROM pg_catalog.pg_attribute a
 JOIN tool_calls tc ON tc.oid = a.attrelid
 WHERE a.attnum > 0 AND NOT a.attisdropped AND a.attnotnull
   AND NOT a.atthasdef AND a.attgenerated = '' AND a.attidentity = ''
   AND a.attname NOT IN ('message_id', 'tool_call_id', 'chat_id', 'project_id')
 )
 AND EXISTS (SELECT 1 FROM tool_call_arbiters WHERE indimmediate AND indisready)
 AND NOT EXISTS (SELECT 1 FROM tool_call_arbiters WHERE NOT indimmediate)`).Scan(&compatible)
	if err != nil {
		return fmt.Errorf("check chat transcript shadow schema: %w", err)
	}
	if !compatible {
		return fmt.Errorf("chat transcript shadow schema is unavailable or incompatible")
	}
	return nil
}

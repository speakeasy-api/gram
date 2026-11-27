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
func configureTranscripts(ctx context.Context, c *cli.Context, db *pgxpool.Pool, writer *chat.ChatMessageWriter, orgEncryption orgEncryptionDeps, logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider) (*transcriptstore.Transcripts, error) {
	var none *transcriptstore.Transcripts
	values := c.StringSlice(chatTranscriptShadowProjectIDsFlag)
	if len(values) == 0 {
		// An explicitly supplied blank must not silently disable the feature.
		if c.IsSet(chatTranscriptShadowProjectIDsFlag) {
			return none, fmt.Errorf("chat transcript shadow mode requires explicit nonempty project UUIDs")
		}
		return none, nil
	}
	projectIDs, err := parseTranscriptShadowProjectIDs(values)
	if err != nil {
		return none, err
	}
	if orgEncryption.Service == nil || orgEncryption.Provisioner == nil {
		return none, fmt.Errorf("chat transcript shadow mode requires organization encryption service and provisioner")
	}
	if db == nil || writer == nil {
		return none, fmt.Errorf("chat transcript shadow mode requires database and writer")
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
	store, err := transcriptstore.New(db, orgEncryption.Service, logger, tracerProvider, meterProvider)
	if err != nil {
		return none, fmt.Errorf("create chat transcript store: %w", err)
	}
	for _, org := range organizations {
		if err := orgEncryption.Provisioner.EnsureUseCase(ctx, org, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()); err != nil {
			return none, fmt.Errorf("provision chat transcript encryption: %w", err)
		}
	}
	transcripts := transcriptstore.NewTranscripts(store, projectIDs...)
	writer.WithTranscripts(transcripts)
	// Sealing runs inside write transactions: keep the transcript key warm so
	// a request does not wait for KMS each time the cached key expires.
	go orgEncryption.Service.KeepWarm(ctx, orgencryption.UseCaseChatTranscripts)
	return transcripts, nil
}

// checkTranscriptSchema checks what the store reads and writes: the sidecar
// and its nullable legacy source_hash, the chat_messages metadata columns, and
// the tool-call ID table with the (message_id, tool_call_id) conflict target
// its inserts use.
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
   AND a.attname NOT IN ('message_id', 'project_id', 'organization_id', 'ciphertext')
 )
 AND EXISTS (SELECT 1 FROM arbiters WHERE indimmediate AND indisready)
 AND NOT EXISTS (SELECT 1 FROM arbiters WHERE NOT indimmediate)
 AND NOT EXISTS (
 -- Resealing clears the legacy source_hash, and reads skip transcripts that
 -- still carry it, so it must exist and be nullable.
 SELECT 1 FROM pg_catalog.pg_attribute a
 JOIN sidecar s ON s.oid = a.attrelid
 WHERE a.attname = 'source_hash' AND NOT a.attisdropped
   AND (a.atttypid <> 'bytea'::regtype OR a.attnotnull)
 )
 AND EXISTS (
 SELECT 1 FROM pg_catalog.pg_attribute a
 JOIN sidecar s ON s.oid = a.attrelid
 WHERE a.attname = 'source_hash' AND NOT a.attisdropped
 )
 AND NOT EXISTS (
 -- Sealing writes the metadata to chat_messages.
 SELECT 1 FROM (VALUES ('tool_call_count', 'integer'::regtype), ('content_bytes', 'integer'::regtype),
   ('content_raw_bytes', 'integer'::regtype), ('tool_calls_bytes', 'integer'::regtype)) AS column_needed(name, type_oid)
 WHERE NOT EXISTS (
  SELECT 1 FROM pg_catalog.pg_attribute a
  JOIN messages m ON m.oid = a.attrelid
  WHERE a.attname = column_needed.name AND a.atttypid = column_needed.type_oid AND NOT a.attisdropped
 ))
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
		return fmt.Errorf("check chat transcript schema: %w", err)
	}
	if !compatible {
		return fmt.Errorf("chat transcript schema is unavailable or incompatible")
	}
	return nil
}

-- name: LockChatMessage :one
-- Reads the whole message row and its current transcript in one statement and
-- locks both for the rest of the caller's transaction, so no protected fields update can
-- land between this read and the copies written from it. A legacy transcript
-- with source_hash is not current: the column is empty for it, as for a message
-- with no transcript (a real ciphertext is never empty). Every writer takes
-- these locks through this statement, so they are always taken in the same
-- order.
SELECT sqlc.embed(m),
       COALESCE((SELECT t.ciphertext FROM chat_message_transcripts t
        WHERE t.message_id = m.id AND t.project_id = m.project_id
          AND t.organization_id = p.organization_id AND t.source_hash IS NULL
        FOR UPDATE), '')::text AS current_ciphertext
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
WHERE m.id = @message_id AND m.project_id = @project_id AND p.organization_id = @organization_id
FOR NO KEY UPDATE OF m
FOR SHARE OF c, p;

-- name: CurrentCiphertext :one
-- The transcript an update decrypts and changes, locked until the transaction
-- ends. Every transcript writer takes LockChatMessage's message lock first, so this
-- lock only states the intent; take them in that order. A legacy transcript
-- with source_hash may be stale, so it is not returned and the update starts
-- from the plaintext row.
SELECT ciphertext FROM chat_message_transcripts
WHERE message_id = @message_id AND project_id = @project_id AND organization_id = @organization_id
  AND source_hash IS NULL
FOR UPDATE;

-- name: WriteChatMessage :one
-- The only statement that changes an existing message's protected fields: the six protected fields
-- columns, plus the non-protected fields the protected fields change may come with. Call it only
-- through Transcripts.UpdateChatMessage, which locks the row, applies the change
-- in Go and writes the encrypted transcript from what this returns, in the
-- same transaction.
UPDATE chat_messages AS m
SET content = @content, content_raw = sqlc.narg('content_raw'), tool_calls = sqlc.narg('tool_calls'),
    tool_outcome_notes = sqlc.narg('tool_outcome_notes'), tool_call_summaries = sqlc.narg('tool_call_summaries'),
    storage_error = sqlc.narg('storage_error'), tool_outcome = sqlc.narg('tool_outcome'),
    finish_reason = sqlc.narg('finish_reason'), content_hash = sqlc.narg('content_hash'),
    risk_analyzed_at = sqlc.narg('risk_analyzed_at')
WHERE m.id = @message_id AND m.project_id = @project_id
RETURNING m.*;

-- name: CanonicalJSON :one
-- The text form Postgres stores for jsonb values, so encrypted-mode protected fields are
-- sealed and measured exactly as a plaintext row would hold them. NULL comes
-- back as the empty string, which no JSON value is.
SELECT COALESCE(sqlc.narg('content_raw')::jsonb::text, '')::text AS content_raw,
       COALESCE(sqlc.narg('tool_calls')::jsonb::text, '')::text AS tool_calls,
       COALESCE(sqlc.narg('tool_call_summaries')::jsonb::text, '')::text AS tool_call_summaries;

-- name: Capture :execrows
-- A resealed transcript drops the legacy source_hash.
INSERT INTO chat_message_transcripts (message_id, project_id, organization_id, ciphertext)
VALUES (@message_id, @project_id, @organization_id, @ciphertext)
ON CONFLICT (message_id) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, source_hash = NULL, updated_at = clock_timestamp()
WHERE chat_message_transcripts.project_id = @project_id AND chat_message_transcripts.organization_id = @organization_id;

-- name: Load :one
-- A transcript that still carries the legacy source_hash was sealed before protected fields
-- updates resealed it, so it may be stale: it reads as missing until backfill
-- reseals it.
SELECT m.id, m.chat_id, m.content, m.content_raw, m.tool_calls,
       m.tool_outcome_notes, m.tool_call_summaries, m.storage_error, t.ciphertext
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND t.organization_id = p.organization_id
WHERE m.id = @message_id AND m.project_id = @project_id AND p.organization_id = @organization_id
  AND t.source_hash IS NULL;

-- name: Backfill :many
SELECT m.id FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
WHERE m.project_id = @project_id AND p.organization_id = @organization_id AND m.id > @after_id
ORDER BY m.id LIMIT @page_limit;

-- name: Progress :one
SELECT count(*) FILTER (WHERE m.project_id = c.project_id AND t.message_id IS NOT NULL AND t.source_hash IS NULL)::bigint AS current,
       count(*) FILTER (WHERE m.project_id = c.project_id AND (t.message_id IS NULL OR t.source_hash IS NOT NULL))::bigint AS waiting,
       count(*) FILTER (WHERE m.project_id IS DISTINCT FROM c.project_id)::bigint AS unattributed
FROM chats c JOIN projects p ON p.id = c.project_id
JOIN chat_messages m ON m.chat_id = c.id
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = c.project_id AND t.organization_id = p.organization_id
WHERE c.project_id = @project_id AND p.organization_id = @organization_id;

-- name: LoadMany :many
-- Legacy transcripts that still carry source_hash read as missing, as in Load.
SELECT m.id, m.chat_id, m.content, m.content_raw, m.tool_calls,
       m.tool_outcome_notes, m.tool_call_summaries, m.storage_error, t.ciphertext
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND t.organization_id = p.organization_id
WHERE m.id = ANY(@message_ids::uuid[]) AND m.project_id = @project_id AND p.organization_id = @organization_id
  AND t.source_hash IS NULL;

-- name: UpdateMessageMetadata :execrows
-- Plaintext-free metadata measured from the snapshot just sealed, in the same
-- transaction, while LockChatMessage's lock keeps the protected fields unchanged.
UPDATE chat_messages
SET tool_call_count = @tool_call_count::integer, content_bytes = @content_bytes::integer,
    content_raw_bytes = @content_raw_bytes::integer, tool_calls_bytes = @tool_calls_bytes::integer
WHERE id = @message_id AND project_id = @project_id::uuid;

-- name: DeleteToolCallIDs :exec
DELETE FROM chat_message_tool_call_ids WHERE message_id = @message_id AND project_id = @project_id;

-- name: InsertToolCallIDs :exec
-- The chat and project come from the message, not from the caller, so a row
-- cannot name another chat or project than its message. The tool-call IDs are
-- the caller's, parsed from the snapshot that was just sealed.
INSERT INTO chat_message_tool_call_ids (message_id, tool_call_id, chat_id, project_id)
SELECT m.id, ids.tool_call_id, m.chat_id, m.project_id
FROM chat_messages m
CROSS JOIN unnest(@tool_call_ids::text[]) AS ids(tool_call_id)
WHERE m.id = @message_id AND m.project_id = @project_id::uuid
ON CONFLICT (message_id, tool_call_id) DO NOTHING;

-- name: SchemaCompatible :one
-- Checks what the store reads and writes: the transcript table and its
-- nullable legacy source_hash, the chat_messages metadata columns, and the
-- tool-call ID table with the (message_id, tool_call_id) conflict target its
-- inserts use. It reads only catalogs, never transcript payloads, and resolves
-- tables through the connection's search_path, as the store's queries do.
-- Every NOT NULL column the store omits must have a database default. Every
-- inferred conflict arbiter must be immediate: PostgreSQL rejects even a
-- redundant deferred one.
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
SELECT (EXISTS (SELECT 1 FROM sidecar)
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
 AND NOT EXISTS (SELECT 1 FROM tool_call_arbiters WHERE NOT indimmediate))::boolean AS compatible;

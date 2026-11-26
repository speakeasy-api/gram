-- name: Snapshot :one
-- Locks the message for the rest of the caller's transaction, so no body update
-- can land between this read and the transcript written from it.
SELECT m.id, m.chat_id, m.content, m.content_raw, m.tool_calls,
       m.tool_outcome_notes, m.tool_call_summaries, m.storage_error
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
WHERE m.id = @message_id AND m.project_id = @project_id AND p.organization_id = @organization_id
FOR NO KEY UPDATE OF m
FOR SHARE OF c, p;

-- name: CurrentCiphertext :one
-- The transcript an update decrypts and changes. Call it after Snapshot, which
-- holds the message lock. A legacy transcript with source_hash may be stale,
-- so it is not returned and the update starts from the plaintext row.
SELECT ciphertext FROM chat_message_transcripts
WHERE message_id = @message_id AND project_id = @project_id AND organization_id = @organization_id
  AND source_hash IS NULL;

-- name: Capture :execrows
-- A resealed transcript drops the legacy source_hash.
INSERT INTO chat_message_transcripts (message_id, project_id, organization_id, ciphertext)
VALUES (@message_id, @project_id, @organization_id, @ciphertext)
ON CONFLICT (message_id) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, source_hash = NULL, updated_at = clock_timestamp()
WHERE chat_message_transcripts.project_id = @project_id AND chat_message_transcripts.organization_id = @organization_id;

-- name: Load :one
-- A transcript that still carries the legacy source_hash was sealed before body
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
-- transaction, while Snapshot's lock keeps the body unchanged.
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

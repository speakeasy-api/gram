-- name: Snapshot :one
SELECT m.id, m.chat_id, m.content, m.content_raw, m.tool_calls,
       m.tool_outcome_notes, m.tool_call_summaries, m.storage_error,
       sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8'))::bytea AS source_hash
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
WHERE m.id = @message_id AND m.project_id = @project_id AND p.organization_id = @organization_id;

-- name: Capture :execrows
WITH live AS MATERIALIZED (
 SELECT m.id FROM chat_messages m
 JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
 JOIN projects p ON p.id = c.project_id
 WHERE m.id = @message_id AND m.project_id = @project_id AND p.organization_id = @organization_id
 AND m.chat_id = @chat_id
 AND sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')) = @source_hash::bytea
 FOR SHARE OF m, c, p
)
INSERT INTO chat_message_transcripts (message_id, project_id, organization_id, ciphertext, source_hash, tool_call_count, content_bytes, content_raw_bytes, tool_calls_bytes)
SELECT live.id, @project_id, @organization_id, @ciphertext, @source_hash::bytea, @tool_call_count::integer, @content_bytes::integer, @content_raw_bytes::integer, @tool_calls_bytes::integer FROM live
ON CONFLICT (message_id) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, source_hash = EXCLUDED.source_hash, tool_call_count = EXCLUDED.tool_call_count, content_bytes = EXCLUDED.content_bytes, content_raw_bytes = EXCLUDED.content_raw_bytes, tool_calls_bytes = EXCLUDED.tool_calls_bytes, updated_at = clock_timestamp()
WHERE chat_message_transcripts.project_id = @project_id AND chat_message_transcripts.organization_id = @organization_id;

-- name: Load :one
SELECT m.id, m.chat_id, m.content, m.content_raw, m.tool_calls,
       m.tool_outcome_notes, m.tool_call_summaries, m.storage_error, t.ciphertext,
       (t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')))::boolean AS current
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND t.organization_id = p.organization_id
WHERE m.id = @message_id AND m.project_id = @project_id AND p.organization_id = @organization_id;

-- name: Backfill :many
SELECT m.id FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
WHERE m.project_id = @project_id AND p.organization_id = @organization_id AND m.id > @after_id
ORDER BY m.id LIMIT @page_limit;

-- name: Progress :one
SELECT count(*) FILTER (WHERE m.project_id = c.project_id AND t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')))::bigint AS current,
       count(*) FILTER (WHERE m.project_id = c.project_id AND (t.message_id IS NULL OR t.source_hash <> sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8'))))::bigint AS waiting,
       count(*) FILTER (WHERE m.project_id IS DISTINCT FROM c.project_id)::bigint AS unattributed
FROM chats c JOIN projects p ON p.id = c.project_id
JOIN chat_messages m ON m.chat_id = c.id
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = c.project_id AND t.organization_id = p.organization_id
WHERE c.project_id = @project_id AND p.organization_id = @organization_id;

-- name: LoadMany :many
SELECT m.id, m.chat_id, m.content, m.content_raw, m.tool_calls,
       m.tool_outcome_notes, m.tool_call_summaries, m.storage_error, t.ciphertext,
       (t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')))::boolean AS current
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND t.organization_id = p.organization_id
WHERE m.id = ANY(@message_ids::uuid[]) AND m.project_id = @project_id AND p.organization_id = @organization_id;

-- name: DeleteToolCallIDs :exec
DELETE FROM chat_message_transcript_tool_calls WHERE message_id = @message_id AND project_id = @project_id;

-- name: InsertToolCallIDs :exec
-- The chat and project come from the captured shadow and its message, not
-- from the caller, so a row cannot name another chat or project than its
-- message. The tool-call IDs are the caller's, parsed from the snapshot that
-- capture just sealed.
INSERT INTO chat_message_transcript_tool_calls (message_id, tool_call_id, chat_id, project_id)
SELECT t.message_id, ids.tool_call_id, m.chat_id, t.project_id
FROM chat_message_transcripts t
JOIN chat_messages m ON m.id = t.message_id AND m.project_id = t.project_id
CROSS JOIN unnest(@tool_call_ids::text[]) AS ids(tool_call_id)
WHERE t.message_id = @message_id AND t.project_id = @project_id
ON CONFLICT (message_id, tool_call_id) DO NOTHING;

-- name: FindIndexedToolCallMessageID :one
-- The newest assistant message of a chat whose shadow indexes the tool call.
-- Reads no plaintext.
SELECT m.id
FROM chat_message_transcript_tool_calls tc
JOIN chat_messages m ON m.id = tc.message_id AND m.project_id = tc.project_id
WHERE tc.chat_id = @chat_id
  AND tc.tool_call_id = @tool_call_id
  AND tc.project_id = @project_id
  AND m.chat_id = @chat_id
  AND m.role = 'assistant'
ORDER BY m.created_at DESC, m.seq DESC, m.id DESC
LIMIT 1;

-- name: LockChatMessage :exec
-- Lock one message for the rest of the transaction. It is a statement of its
-- own, so the read that follows takes a fresh snapshot and sees the message
-- and its shadow as the previous lock holder committed them. A locking read
-- that joined the shadow would recheck only the message row after waiting and
-- pair it with the shadow it saw before.
SELECT 1 FROM chat_messages WHERE project_id = @project_id::uuid AND id = @id FOR UPDATE;

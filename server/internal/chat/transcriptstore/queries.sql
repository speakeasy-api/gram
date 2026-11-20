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
INSERT INTO chat_message_transcripts (message_id, project_id, organization_id, ciphertext, source_hash)
SELECT live.id, @project_id, @organization_id, @ciphertext, @source_hash::bytea FROM live
ON CONFLICT (message_id) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, source_hash = EXCLUDED.source_hash, updated_at = now()
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
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND t.organization_id = p.organization_id
WHERE m.project_id = @project_id AND p.organization_id = @organization_id AND m.id > @after_id
AND (t.message_id IS NULL OR t.source_hash <> sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')))
ORDER BY m.id LIMIT @page_limit;

-- name: Progress :one
SELECT count(*) FILTER (WHERE m.project_id = c.project_id AND t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')))::bigint AS current,
       count(*) FILTER (WHERE m.project_id = c.project_id AND (t.message_id IS NULL OR t.source_hash <> sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8'))))::bigint AS waiting,
       count(*) FILTER (WHERE m.project_id IS DISTINCT FROM c.project_id)::bigint AS unattributed
FROM chats c JOIN projects p ON p.id = c.project_id
JOIN chat_messages m ON m.chat_id = c.id
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = c.project_id AND t.organization_id = p.organization_id
WHERE c.project_id = @project_id AND p.organization_id = @organization_id;

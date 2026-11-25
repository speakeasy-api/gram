-- Every query that reads the plaintext transcript columns of chat_messages
-- (content, content_raw, tool_calls, tool_outcome_notes, tool_call_summaries,
-- storage_error) lives here, inside transcriptstore/internal, so the Go
-- compiler refuses to let any other package call one. Each read LEFT JOINs the
-- message's encrypted shadow when @open_shadows is true (the project keeps
-- shadows), returning the ciphertext and whether its source hash still
-- matches in the same round trip. When it is false the join is skipped by a
-- one-time filter and costs nothing.

-- name: GetProjectOrganizationID :one
SELECT organization_id FROM projects WHERE id = @project_id;

-- name: ListLatestGenerationChatMessages :many
-- Returns only the latest-generation rows; older generations are audit-only.
SELECT sqlc.embed(m), t.ciphertext AS shadow_ciphertext,
       COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.generation = (SELECT MAX(generation) FROM chat_messages WHERE chat_id = @chat_id AND project_id = @project_id::uuid)
ORDER BY m.created_at ASC, m.seq ASC;

-- name: ListChatMessagesByGeneration :many
-- Returns rows for an explicit generation, used to pin a snapshot across
-- multiple reads (e.g. across Temporal activities) so indices stay stable
-- even if a new generation is appended mid-workflow.
SELECT sqlc.embed(m), t.ciphertext AS shadow_ciphertext,
       COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.generation = @generation::integer
ORDER BY m.created_at ASC, m.seq ASC;

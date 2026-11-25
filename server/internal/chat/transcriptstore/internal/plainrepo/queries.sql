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


-- name: ListChatMessagesAfterPage :many
-- Keyset page within a generation, oldest first. Same anchor-row cursor
-- resolution as ListChatMessagesBeforePage. Returns rows strictly after the
-- anchor in transcript order, or the oldest page (start of the thread) when
-- @after_seq is NULL. Fetch @lim = pageSize+1 to detect whether more newer
-- rows remain.
SELECT sqlc.embed(m), t.ciphertext AS shadow_ciphertext,
       COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.generation = @generation::integer
  AND (
    sqlc.narg('after_seq')::bigint IS NULL
    OR (m.created_at, m.seq) > (
      SELECT a.created_at, a.seq FROM chat_messages a
      WHERE a.chat_id = @chat_id
        AND a.project_id = @project_id::uuid
        AND a.seq = sqlc.narg('after_seq')::bigint
    )
    -- Same missing-anchor fallback as ListChatMessagesBeforePage.
    OR (
      NOT EXISTS (
        SELECT 1 FROM chat_messages a
        WHERE a.chat_id = @chat_id
          AND a.project_id = @project_id::uuid
          AND a.seq = sqlc.narg('after_seq')::bigint
      )
      AND m.seq > sqlc.narg('after_seq')::bigint
    )
  )
ORDER BY m.created_at ASC, m.seq ASC
LIMIT @lim::integer;

-- name: ListChatMessagesBeforePage :many
-- Keyset page within a generation, newest first. The cursor stays the anchor
-- row's seq (stable, unique, already in the API), but position is compared in
-- transcript order (created_at, seq) so pages agree with the display order
-- even when spool-replayed rows carry backdated created_at. Returns rows
-- strictly before the anchor, or the newest page when @before_seq is NULL.
-- Order DESC so LIMIT keeps the most recent rows; the caller reverses to
-- ascending for display. Fetch @lim = pageSize+1 to detect whether more older
-- rows remain.
SELECT sqlc.embed(m), t.ciphertext AS shadow_ciphertext,
       COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.generation = @generation::integer
  AND (
    sqlc.narg('before_seq')::bigint IS NULL
    OR (m.created_at, m.seq) < (
      SELECT a.created_at, a.seq FROM chat_messages a
      WHERE a.chat_id = @chat_id
        AND a.project_id = @project_id::uuid
        AND a.seq = sqlc.narg('before_seq')::bigint
    )
    -- A cursor whose anchor row no longer resolves must not dead-end the
    -- page (a tuple comparison against an empty subquery is NULL): fall
    -- back to the plain seq comparison the pre-tuple cursor used.
    OR (
      NOT EXISTS (
        SELECT 1 FROM chat_messages a
        WHERE a.chat_id = @chat_id
          AND a.project_id = @project_id::uuid
          AND a.seq = sqlc.narg('before_seq')::bigint
      )
      AND m.seq < sqlc.narg('before_seq')::bigint
    )
  )
ORDER BY m.created_at DESC, m.seq DESC
LIMIT @lim::integer;

-- name: ListRiskWindowedMessages :many
-- Risk-only view: returns every message within +/- @context_size ordinal
-- positions of any active risk finding in the generation, ordered oldest to
-- newest. rn is the message's 1-based ordinal within the generation and total
-- is the generation's message count, so the caller can fold consecutive rn into
-- contiguous segments and decide whether earlier (rn > 1) or later (rn < total)
-- messages remain to be expanded. Overlapping windows merge naturally via set
-- membership. is_risk flags the seed rows (the flagged messages themselves) so
-- the caller can return the explicit risk seq list (context rows are
-- is_risk = false).
WITH ordered AS (
  SELECT
    cm.*,
    row_number() OVER (ORDER BY cm.created_at, cm.seq) AS rn,
    count(*) OVER () AS total
  FROM chat_messages cm
  WHERE cm.chat_id = @chat_id
    AND cm.project_id = @project_id::uuid
    AND cm.generation = @generation::integer
),
-- Prompts whose attachments hold an active finding. Such a finding has no
-- chat_message_id, so it flags the prompt the attachment hangs off.
attachment_risk_prompts AS (
  SELECT ccp.parent_chat_message_id AS id
  FROM chat_content_parts ccp
  WHERE ccp.chat_id = @chat_id
    AND ccp.project_id = @project_id::uuid
    AND ccp.deleted IS FALSE
    AND ccp.parent_chat_message_id IS NOT NULL
    AND ccp.id = ANY(@risk_content_part_ids::uuid[])
),
risk_rns AS (
  SELECT o.rn FROM ordered o
  WHERE o.id = ANY(@risk_message_ids::uuid[])
  OR o.id IN (SELECT id FROM attachment_risk_prompts)
)
SELECT
  o.*,
  EXISTS (SELECT 1 FROM risk_rns r WHERE r.rn = o.rn) AS is_risk,
  t.ciphertext AS shadow_ciphertext,
       COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,o.content,o.content_raw IS NULL,o.content_raw,o.tool_calls IS NULL,o.tool_calls,o.tool_outcome_notes,o.tool_call_summaries IS NULL,o.tool_call_summaries,o.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM ordered o
LEFT JOIN chat_message_transcripts t ON t.message_id = o.id AND t.project_id = o.project_id AND @open_shadows::boolean
WHERE EXISTS (
  SELECT 1 FROM risk_rns r
  WHERE o.rn BETWEEN r.rn - @context_size::bigint AND r.rn + @context_size::bigint
)
ORDER BY o.created_at ASC, o.seq ASC;

-- name: ListSearchWindowedMessages :many
-- Query-search view: same windowing as ListRiskWindowedMessages, but the seed
-- rows are messages whose searchable text matches @query (case-insensitive
-- substring over the narrative content, the tool-call name/arguments JSON, and
-- any structured/multimodal content) instead of messages with a risk finding.
-- Each match is padded with +/- @context_size ordinal positions. rn/total drive
-- segment folding and the has_more flags; is_match flags the seed rows so the
-- caller can return the explicit jump-to-match seq list (context rows are
-- is_match = false). Seed matches are capped at @match_limit (earliest first by
-- ordinal) to bound the response on broad queries. Asset-offloaded content (too
-- large to store inline) is not searchable here.
WITH ordered AS (
  SELECT
    cm.*,
    row_number() OVER (ORDER BY cm.created_at, cm.seq) AS rn,
    count(*) OVER () AS total
  FROM chat_messages cm
  WHERE cm.chat_id = @chat_id
    AND cm.project_id = @project_id::uuid
    AND cm.generation = @generation::integer
),
match_rns AS (
  SELECT o.rn FROM ordered o
  WHERE o.content ILIKE '%' || @query::text || '%'
     OR (o.tool_calls IS NOT NULL AND o.tool_calls::text ILIKE '%' || @query::text || '%')
     OR (o.content_raw IS NOT NULL AND o.content_raw::text ILIKE '%' || @query::text || '%')
  ORDER BY o.rn
  LIMIT @match_limit::integer
)
SELECT
  o.*,
  EXISTS (SELECT 1 FROM match_rns m WHERE m.rn = o.rn) AS is_match,
  t.ciphertext AS shadow_ciphertext,
       COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,o.content,o.content_raw IS NULL,o.content_raw,o.tool_calls IS NULL,o.tool_calls,o.tool_outcome_notes,o.tool_call_summaries IS NULL,o.tool_call_summaries,o.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM ordered o
LEFT JOIN chat_message_transcripts t ON t.message_id = o.id AND t.project_id = o.project_id AND @open_shadows::boolean
WHERE EXISTS (
  SELECT 1 FROM match_rns m
  WHERE o.rn BETWEEN m.rn - @context_size::bigint AND m.rn + @context_size::bigint
)
ORDER BY o.created_at ASC, o.seq ASC;

-- name: GetChatEntryTotals :one
-- Per-generation trace-entry totals for the chat detail filter bar. The detail
-- sheet paginates messages, so counts derived from the loaded page understate
-- the chat; these totals describe the whole generation regardless of which page
-- is in view. Each message maps to exactly one entry, mirroring the client's
-- getTraceEntryType precedence: a message carrying a non-empty tool_calls array
-- is a tool call regardless of role, otherwise the role decides. risk_findings
-- counts messages the caller's ClickHouse finding anchors flag
-- (@risk_message_ids), directly or through an attachment hanging off them
-- (@risk_content_part_ids).
WITH ordered AS (
  SELECT
    cm.id,
    cm.role,
    CASE
      WHEN cm.tool_calls IS NULL THEN false
      WHEN jsonb_typeof(cm.tool_calls) = 'array'
        THEN jsonb_array_length(cm.tool_calls) > 0
      -- Some rows store tool_calls double-encoded (a JSON string holding the
      -- array); treat any non-empty/non-"[]" string as carrying tool calls.
      WHEN jsonb_typeof(cm.tool_calls) = 'string'
        THEN btrim(cm.tool_calls #>> '{}') NOT IN ('', '[]', 'null')
      ELSE false
    END AS has_tool_calls
  FROM chat_messages cm
  WHERE cm.chat_id = @chat_id
    AND cm.project_id = @project_id::uuid
    AND cm.generation = @generation::integer
),
-- Prompts whose attachments hold an active finding. Such a finding has no
-- chat_message_id, so it flags the prompt the attachment hangs off.
attachment_risk_prompts AS (
  SELECT ccp.parent_chat_message_id AS id
  FROM chat_content_parts ccp
  WHERE ccp.chat_id = @chat_id
    AND ccp.project_id = @project_id::uuid
    AND ccp.deleted IS FALSE
    AND ccp.parent_chat_message_id IS NOT NULL
    AND ccp.id = ANY(@risk_content_part_ids::uuid[])
)
SELECT
  COUNT(*) FILTER (WHERE has_tool_calls OR role IN ('user', 'assistant', 'tool'))::bigint AS total,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'user')::bigint AS user_messages,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'assistant')::bigint AS assistant_messages,
  COUNT(*) FILTER (WHERE has_tool_calls)::bigint AS tool_calls,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'tool')::bigint AS tool_results,
  (
    SELECT COUNT(*)::bigint FROM ordered o
    WHERE o.id = ANY(@risk_message_ids::uuid[])
    OR o.id IN (SELECT id FROM attachment_risk_prompts)
  )::bigint AS risk_findings
FROM ordered;

-- name: ListChatMessagesForMatch :many
SELECT m.id, m.role, m.content, m.tool_call_id, m.tool_calls, m.original_message_id,
  COALESCE(m.capture_kind, CASE WHEN m.generation = 0 THEN 'original' ELSE 'unknown' END)::text AS capture_kind,
  t.ciphertext AS shadow_ciphertext,
  COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.generation = @generation
ORDER BY m.created_at ASC, m.seq ASC;

-- name: GetKitProjectionMessage :one
SELECT sqlc.embed(m), t.ciphertext AS shadow_ciphertext,
  COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.project_id = @project_id AND m.id = @id;

-- name: GetToolCallSummaryContext :one
-- Fetch the owning call message and its matching result within one project and
-- chat. The handler validates that tool_calls contains the requested call id.
SELECT
  calls.chat_id,
  calls.tool_calls,
  calls.tool_call_summaries,
  results.id AS result_message_id,
  results.content::text AS result_content,
  tc.ciphertext AS calls_shadow_ciphertext,
  COALESCE(tc.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,calls.content,calls.content_raw IS NULL,calls.content_raw,calls.tool_calls IS NULL,calls.tool_calls,calls.tool_outcome_notes,calls.tool_call_summaries IS NULL,calls.tool_call_summaries,calls.storage_error)::text,'UTF8')), false)::boolean AS calls_shadow_current,
  tr.ciphertext AS result_shadow_ciphertext,
  COALESCE(tr.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,results.content,results.content_raw IS NULL,results.content_raw,results.tool_calls IS NULL,results.tool_calls,results.tool_outcome_notes,results.tool_call_summaries IS NULL,results.tool_call_summaries,results.storage_error)::text,'UTF8')), false)::boolean AS result_shadow_current
FROM chat_messages AS calls
JOIN chat_messages AS results
  ON results.chat_id = calls.chat_id
  AND results.project_id = calls.project_id
  AND results.tool_call_id = @tool_call_id
  AND results.generation = calls.generation
  AND results.role = 'tool'
LEFT JOIN chat_message_transcripts tc ON tc.message_id = calls.id AND tc.project_id = calls.project_id AND @open_shadows::boolean
LEFT JOIN chat_message_transcripts tr ON tr.message_id = results.id AND tr.project_id = results.project_id AND @open_shadows::boolean
WHERE calls.id = @message_id
  AND calls.chat_id = @chat_id
  AND calls.project_id = @project_id::uuid
  AND calls.role = 'assistant'
ORDER BY results.created_at ASC, results.seq ASC, results.id ASC
LIMIT 1;

-- name: ListClaudeUserMessagesForPromptAttachmentParent :many
SELECT m.id, m.content, t.ciphertext AS shadow_ciphertext,
  COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,m.content,m.content_raw IS NULL,m.content_raw,m.tool_calls IS NULL,m.tool_calls,m.tool_outcome_notes,m.tool_call_summaries IS NULL,m.tool_call_summaries,m.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.role = 'user'
  AND m.content != ''
ORDER BY m.seq DESC, m.created_at DESC;

-- name: ListChatTranscriptMessagesPage :many
-- Keyset page of one chat's messages, newest first, carrying only the columns
-- transcript rendering reads. A transcript reader that pulled the whole chat
-- held every message of an unbounded session in memory to then throw most of
-- them away, so it walks backwards a page at a time instead and stops as soon
-- as the rendering starts dropping messages: the trim is oldest-first, so every
-- unread row is older than the ones already being dropped and would be dropped
-- too.
--
-- The same project filter as ListChatMessages, so a page can never cross a
-- project boundary. CountChatMessages supplies the total once per transcript;
-- repeating a windowed count on every page makes long transcripts quadratic.
--
-- The cursor is the full transcript key (created_at, seq, id): created_at and
-- seq alone are not unique, and a tie split across a page boundary would either
-- repeat or skip a message.
SELECT
  cm.id,
  cm.seq,
  cm.created_at,
  cm.role,
  cm.content,
  cm.tool_calls,
  cm.tool_call_id,
  cm.tool_urn,
  cm.tool_outcome,
  cm.tool_outcome_notes,
  t.ciphertext AS shadow_ciphertext,
  COALESCE(t.source_hash = sha256(convert_to(jsonb_build_array('chat-transcript-source/v2'::text,cm.content,cm.content_raw IS NULL,cm.content_raw,cm.tool_calls IS NULL,cm.tool_calls,cm.tool_outcome_notes,cm.tool_call_summaries IS NULL,cm.tool_call_summaries,cm.storage_error)::text,'UTF8')), false)::boolean AS shadow_current
FROM chat_messages cm
LEFT JOIN chat_message_transcripts t ON t.message_id = cm.id AND t.project_id = cm.project_id AND @open_shadows::boolean
WHERE cm.chat_id = @chat_id
  AND cm.project_id = @project_id::uuid
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (cm.created_at, cm.seq, cm.id) < (
      sqlc.narg('cursor_created_at')::timestamptz,
      sqlc.narg('cursor_seq')::bigint,
      sqlc.narg('cursor_id')::uuid
    )
  )
ORDER BY cm.created_at DESC, cm.seq DESC, cm.id DESC
LIMIT @lim::integer;

-- name: PatchKitProjectionMessage :execrows
UPDATE chat_messages SET content = @content, content_raw = @content_raw,
 tool_calls = @tool_calls, content_hash = @content_hash, finish_reason = @finish_reason,
 risk_analyzed_at = CASE WHEN content IS DISTINCT FROM @content OR content_raw IS DISTINCT FROM @content_raw OR tool_calls IS DISTINCT FROM @tool_calls THEN NULL ELSE risk_analyzed_at END
WHERE project_id = @project_id AND chat_id = @chat_id AND id = @id AND source = 'kit'
AND (content IS DISTINCT FROM @content OR content_raw IS DISTINCT FROM @content_raw OR tool_calls IS DISTINCT FROM @tool_calls OR finish_reason IS DISTINCT FROM @finish_reason);

-- name: UpsertCorrelatedChatMessage :one
-- Returns persisted metering fields and distinguishes initial inserts from
-- native-hook promotions, which must not emit another storage reading.
INSERT INTO chat_messages (
    id
  , chat_id
  , role
  , project_id
  , content
  , content_raw
  , content_asset_url
  , storage_error
  , model
  , message_id
  , tool_call_id
  , user_id
  , external_user_id
  , external_message_id
  , finish_reason
  , tool_calls
  , prompt_tokens
  , completion_tokens
  , total_tokens
  , origin
  , user_agent
  , ip_address
  , source
  , content_hash
  , generation
  , replayed
  , created_at
)
VALUES (
    @id
  , @chat_id
  , @role
  , @project_id::uuid
  , @content
  , @content_raw
  , @content_asset_url
  , @storage_error
  , @model
  , @message_id
  , @tool_call_id
  , @user_id
  , @external_user_id
  , @external_message_id
  , @finish_reason
  , @tool_calls
  , @prompt_tokens
  , @completion_tokens
  , @total_tokens
  , @origin
  , @user_agent
  , @ip_address
  , @source
  , @content_hash
  , @generation
  , @replayed
  , @created_at
)
ON CONFLICT (chat_id, external_message_id) WHERE external_message_id IS NOT NULL
DO UPDATE SET
    source = EXCLUDED.source
  , user_id = COALESCE(EXCLUDED.user_id, chat_messages.user_id)
  , external_user_id = COALESCE(EXCLUDED.external_user_id, chat_messages.external_user_id)
  , model = COALESCE(EXCLUDED.model, chat_messages.model)
  , replayed = chat_messages.replayed OR EXCLUDED.replayed
  , created_at = EXCLUDED.created_at
  , risk_analyzed_at = NULL
WHERE chat_messages.project_id = EXCLUDED.project_id
  AND EXCLUDED.source IN ('codex', 'opencode', 'openclaw')
  AND chat_messages.source = 'litellm'
RETURNING id, content, tool_calls, model, user_id, external_user_id, source, user_agent, (xmax = 0) AS inserted;

-- name: StoreToolCallSummary :one
-- Preserve summaries for sibling calls carried by the same assistant message.
UPDATE chat_messages
SET tool_call_summaries = jsonb_set(
  COALESCE(tool_call_summaries, '{}'::jsonb),
  ARRAY[@tool_call_id::text],
  @summary::jsonb
)
WHERE id = @message_id
  AND chat_id = @chat_id
  AND project_id = @project_id::uuid
RETURNING (tool_call_summaries -> @tool_call_id::text)::text AS summary;

-- name: UpdateToolCallOutcome :exec
UPDATE chat_messages
SET tool_outcome = @tool_outcome,
    tool_outcome_notes = @tool_outcome_notes
WHERE id = @id AND project_id = @project_id::uuid;

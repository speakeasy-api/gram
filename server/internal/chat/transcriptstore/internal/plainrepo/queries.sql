-- name: GetProjectOrganizationID :one
-- The organization a listed project's shadows are sealed under. The package
-- comment in doc.go describes the queries in this file.
SELECT organization_id FROM projects WHERE id = @project_id;

-- name: ListLatestGenerationChatMessages :many
-- Returns only the latest-generation rows; older generations are audit-only.
SELECT sqlc.embed(m), t.ciphertext AS shadow_ciphertext,
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.generation = (SELECT MAX(generation) FROM chat_messages WHERE chat_id = @chat_id AND project_id = @project_id::uuid)
ORDER BY m.created_at ASC, m.seq ASC;

-- name: ListChatMessagesByGeneration :many
-- Returns rows for an explicit generation, used to pin a snapshot across
-- multiple reads (e.g. across Temporal activities) so indices stay stable
-- even if a new generation is appended mid-workflow.
SELECT sqlc.embed(m), t.ciphertext AS shadow_ciphertext,
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
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
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
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
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
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
/* Prompts whose attachments hold an active finding. Such a finding has no
   chat_message_id, so it flags the prompt the attachment hangs off. */
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
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM ordered o
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = o.id AND t.project_id = o.project_id AND @open_shadows::boolean
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
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM ordered o
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = o.id AND t.project_id = o.project_id AND @open_shadows::boolean
WHERE EXISTS (
  SELECT 1 FROM match_rns m
  WHERE o.rn BETWEEN m.rn - @context_size::bigint AND m.rn + @context_size::bigint
)
ORDER BY o.created_at ASC, o.seq ASC;

-- name: ScanChatSearchChunk :many
-- One chunk of a chunked search over one chat generation in a project that
-- keeps shadows: up to @chunk_size messages after the keyset cursor, oldest
-- first. A message with a shadow returns its ciphertext and no plaintext body,
-- so the store matches it on the opened shadow only. A message with no shadow
-- (written before capture began) returns its plaintext, the fallback every read
-- applies. The role lets the store count scanned messages the way
-- GetChatEntryTotals counts the generation, so progress compares like with
-- like.
SELECT m.id, m.chat_id, m.created_at, m.seq, m.role,
       t.ciphertext AS shadow_ciphertext,
       COALESCE(CASE WHEN t.message_id IS NULL THEN m.content END, '')::text AS fallback_content,
       CASE WHEN t.message_id IS NULL THEN m.tool_calls END::jsonb AS fallback_tool_calls,
       CASE WHEN t.message_id IS NULL THEN m.content_raw END::jsonb AS fallback_content_raw
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.generation = @generation::integer
  AND (NOT @has_cursor::boolean OR (m.created_at, m.seq) > (@cursor_at::timestamptz, @cursor_seq::bigint))
ORDER BY m.created_at ASC, m.seq ASC
LIMIT @chunk_size::integer;

-- name: ListSearchMatchWindows :many
-- The search view around matches the store already found in Go: the same rows
-- and columns as ListSearchWindowedMessages, seeded by @match_ids instead of
-- an ILIKE. A chunked search builds windows one chunk at a time, so when this
-- chunk's first window starts right after the previous chunk's last window
-- (@bridge_rn), that row is returned too: the two windows then share a message
-- and the caller joins them into one segment instead of showing a gap.
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
  SELECT o.rn FROM ordered o WHERE o.id = ANY(@match_ids::uuid[])
),
bridge_rns AS (
  SELECT o.rn FROM ordered o
  WHERE o.rn = @bridge_rn::bigint
    AND EXISTS (SELECT 1 FROM match_rns m WHERE m.rn = o.rn + 1 + @context_size::bigint)
)
SELECT
  o.*,
  EXISTS (SELECT 1 FROM match_rns m WHERE m.rn = o.rn) AS is_match,
  t.ciphertext AS shadow_ciphertext,
  (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM ordered o
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = o.id AND t.project_id = o.project_id AND @open_shadows::boolean
WHERE EXISTS (
  SELECT 1 FROM match_rns m
  WHERE o.rn BETWEEN m.rn - @context_size::bigint AND m.rn + @context_size::bigint
)
   OR o.rn IN (SELECT rn FROM bridge_rns)
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
/* Prompts whose attachments hold an active finding. Such a finding has no
   chat_message_id, so it flags the prompt the attachment hangs off. */
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
  (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.chat_id = @chat_id
  AND m.project_id = @project_id::uuid
  AND m.generation = @generation
ORDER BY m.created_at ASC, m.seq ASC;

-- name: GetKitProjectionMessage :one
SELECT sqlc.embed(m), t.ciphertext AS shadow_ciphertext,
  (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
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
  (tc.ciphertext IS NOT NULL)::boolean AS calls_shadow_current,
  tr.ciphertext AS result_shadow_ciphertext,
  (tr.ciphertext IS NOT NULL)::boolean AS result_shadow_current
FROM chat_messages AS calls
JOIN chat_messages AS results
  ON results.chat_id = calls.chat_id
  AND results.project_id = calls.project_id
  AND results.tool_call_id = @tool_call_id
  AND results.generation = calls.generation
  AND results.role = 'tool'
LEFT JOIN chat_message_transcripts tc ON tc.source_hash IS NULL AND tc.message_id = calls.id AND tc.project_id = calls.project_id AND @open_shadows::boolean
LEFT JOIN chat_message_transcripts tr ON tr.source_hash IS NULL AND tr.message_id = results.id AND tr.project_id = results.project_id AND @open_shadows::boolean
WHERE calls.id = @message_id
  AND calls.chat_id = @chat_id
  AND calls.project_id = @project_id::uuid
  AND calls.role = 'assistant'
ORDER BY results.created_at ASC, results.seq ASC, results.id ASC
LIMIT 1;

-- name: ListClaudeUserMessagesForPromptAttachmentParent :many
SELECT m.id, m.content, t.ciphertext AS shadow_ciphertext,
  (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
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
  (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages cm
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = cm.id AND t.project_id = cm.project_id AND @open_shadows::boolean
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

-- name: UpsertCorrelatedChatMessage :one
-- Only its generated params and row types are used: they are the shape of
-- Transcripts.UpsertCorrelatedChatMessage, which runs the repo query of the
-- same name so every write seals and stores metadata. Do not call it.
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

-- name: GetMessageContentBatch :many
-- The scanned user's id rides along so the LLM judge's completion telemetry
-- can attribute scanning volume to whose traffic was analyzed. Same
-- attribution rule as the risk overview top users: the message's own user_id
-- wins, the chat owner's is the fallback — and a soft-deleted chat's owner
-- never is, while its messages remain eligible for scanning.
-- The lateral probes capture bounded causal context at the same event read:
-- the latest preceding user request and the latest tool result strictly after
-- that request. chat_messages_chat_id_created_at_idx supports both reverse
-- time probes; seq deterministically orders equal timestamps.
--
-- created_at bounds the shadow-MCP scanner's ClickHouse provenance lookup to
-- the batch's own time range, keeping that query on the telemetry table's
-- time-ordered primary key instead of scanning the full retention window.
--
-- source names the agent that recorded the message (Codex, Cursor, the ingest
-- adapter, ...). The shadow-MCP scanner attributes its provenance-resolution
-- metric to it, which is the one attribution available when the call resolved
-- to no telemetry row at all — precisely the population that metric exists to
-- measure.
-- Both message queries validate their chat against the row's project before
-- returning linkage. Content-part parents additionally must belong to the
-- content part's chat, so corrupt cross-tenant or cross-chat ids never leave
-- this boundary.
SELECT cm.id, cm.chat_id, cm.role, cm.content, cm.tool_calls, cm.created_at, cm.source,
  COALESCE(NULLIF(cm.user_id, ''), CASE WHEN c.deleted IS FALSE THEN NULLIF(c.user_id, '') END, '')::TEXT AS chat_user_id,
  COALESCE(prior_user_request.content, '')::TEXT AS prior_user_request,
  COALESCE(recent_tool.content, '')::TEXT AS recent_untrusted_content,
  COALESCE(prior_user_request.id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS prior_user_request_id,
  COALESCE(recent_tool.id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS recent_untrusted_content_id,
  t.ciphertext AS shadow_ciphertext,
  (t.ciphertext IS NOT NULL)::boolean AS shadow_current,
  tp.ciphertext AS prior_shadow_ciphertext,
  (tp.ciphertext IS NOT NULL)::boolean AS prior_shadow_current,
  tr.ciphertext AS recent_shadow_ciphertext,
  (tr.ciphertext IS NOT NULL)::boolean AS recent_shadow_current
FROM chat_messages cm
JOIN chats c ON c.id = cm.chat_id
  AND c.project_id = cm.project_id
LEFT JOIN LATERAL (
  SELECT prev.id, LEFT(prev.content, 4000) AS content, prev.created_at, prev.seq
  FROM chat_messages prev
  WHERE prev.chat_id = cm.chat_id
    AND prev.project_id = cm.project_id
    AND prev.role = 'user'
    AND (prev.created_at, prev.seq) < (cm.created_at, cm.seq)
  ORDER BY prev.created_at DESC, prev.seq DESC
  LIMIT 1
) prior_user_request ON TRUE
LEFT JOIN LATERAL (
  SELECT prev.id, LEFT(prev.content, 4000) AS content
  FROM chat_messages prev
  WHERE prev.chat_id = cm.chat_id
    AND prev.project_id = cm.project_id
    AND prev.role = 'tool'
    AND (prev.created_at, prev.seq) < (cm.created_at, cm.seq)
    AND (prev.created_at, prev.seq) > (prior_user_request.created_at, prior_user_request.seq)
  ORDER BY prev.created_at DESC, prev.seq DESC
  LIMIT 1
) recent_tool ON TRUE
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = cm.id AND t.project_id = cm.project_id AND @open_shadows::boolean
LEFT JOIN chat_message_transcripts tp ON tp.source_hash IS NULL AND tp.message_id = prior_user_request.id AND tp.project_id = cm.project_id AND @open_shadows::boolean
LEFT JOIN chat_messages mp ON mp.id = tp.message_id
LEFT JOIN chat_message_transcripts tr ON tr.source_hash IS NULL AND tr.message_id = recent_tool.id AND tr.project_id = cm.project_id AND @open_shadows::boolean
LEFT JOIN chat_messages mr ON mr.id = tr.message_id
WHERE cm.id = ANY(@ids::uuid[])
  AND cm.project_id = @project_id;

-- name: GetChatMessageForUnmask :one
-- Source material for the ClickHouse-backed reveal path: the anchored chat
-- message's recorded content and tool calls, which the reveal reconstructs the
-- raw match from (see unmask_ch.go). role gates the scan-surface composition
-- exactly like batch analysis (only assistant tool-request messages compose
-- tool-call arguments into the scanned text). chat_id backs the chat:read
-- authorization check when the ClickHouse row carries no denormalized chat id.
SELECT cm.id, cm.chat_id, cm.role, cm.content, cm.tool_calls, cm.created_at,
  t.ciphertext AS shadow_ciphertext,
  (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages cm
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = cm.id AND t.project_id = cm.project_id AND @open_shadows::boolean
WHERE cm.id = @id
  AND cm.project_id = @project_id
  -- Nothing in the schema ties a message's project_id to its chat's, so a
  -- message pointing at a chat in another project is rejected outright: the
  -- chat:read gate is evaluated against the finding's chat id, and serving
  -- content anchored in a foreign chat would bypass it.
  AND EXISTS (
    SELECT 1
    FROM chats c
    WHERE c.id = cm.chat_id
      AND c.project_id = cm.project_id
  );

-- name: GetJudgeMessageWindow :many
-- Two bounded index probes around a tenant-validated anchor. Keep the target
-- even when no neighbors exist; an absent target is never a valid window.
-- The window holds at most five messages, returned whole: the judge reads at
-- most 4001 characters of each body and nine tool calls, and transcriptstore
-- applies those caps in Go after opening the shadows, so plaintext and shadows
-- are capped the same way.
WITH target AS (
  SELECT cm.id, cm.chat_id, cm.project_id, cm.role, cm.content, cm.tool_calls, cm.created_at, cm.seq, cm.generation FROM chat_messages cm
  JOIN chats c ON c.id = cm.chat_id AND c.project_id = cm.project_id
  JOIN projects p ON p.id = c.project_id
  WHERE cm.id = @anchor_id::uuid AND cm.project_id = @project_id::uuid
    AND p.organization_id = @organization_id::text
)
SELECT evidence.id, evidence.chat_id, evidence.role, evidence.content, evidence.tool_calls,
  t.ciphertext AS shadow_ciphertext,
  (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM (
SELECT n.id, n.chat_id, n.project_id, n.role, n.content, n.tool_calls, n.created_at, n.seq FROM target t
JOIN LATERAL (
  (SELECT prev.id, prev.chat_id, prev.project_id, prev.role, prev.content, prev.tool_calls, prev.created_at, prev.seq FROM chat_messages prev
   WHERE prev.chat_id = t.chat_id AND prev.project_id = t.project_id AND prev.generation = t.generation
     AND (prev.created_at, prev.seq) < (t.created_at, t.seq)
   ORDER BY prev.created_at DESC, prev.seq DESC LIMIT 2)
  UNION ALL
  SELECT t.id, t.chat_id, t.project_id, t.role, t.content, t.tool_calls, t.created_at, t.seq
  UNION ALL
  (SELECT next.id, next.chat_id, next.project_id, next.role, next.content, next.tool_calls, next.created_at, next.seq FROM chat_messages next
   WHERE next.chat_id = t.chat_id AND next.project_id = t.project_id AND next.generation = t.generation
     AND (next.created_at, next.seq) > (t.created_at, t.seq)
   ORDER BY next.created_at, next.seq LIMIT 2)
) n ON TRUE
UNION ALL
(SELECT recent.id, recent.chat_id, recent.project_id, recent.role, recent.content, recent.tool_calls, recent.created_at, recent.seq FROM chat_messages recent
 JOIN chats c ON c.id = recent.chat_id AND c.project_id = recent.project_id
 JOIN projects p ON p.id = c.project_id
 WHERE @anchor_id::uuid = '00000000-0000-0000-0000-000000000000'::uuid
   AND recent.chat_id = @chat_id::uuid AND recent.project_id = @project_id::uuid
   AND p.organization_id = @organization_id::text
   AND recent.generation = (SELECT MAX(latest.generation) FROM chat_messages latest
     WHERE latest.chat_id = @chat_id::uuid AND latest.project_id = @project_id::uuid)
 ORDER BY recent.created_at DESC, recent.seq DESC LIMIT 4)
) evidence
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = evidence.id AND t.project_id = evidence.project_id AND @open_shadows::boolean
LEFT JOIN chat_messages m ON m.id = t.message_id
ORDER BY evidence.created_at, evidence.seq;

-- name: SearchPlatformChatMessages :many
-- Literal substring search over captured messages in one project and interval,
-- newest or oldest first by (created_at, id). Fields over 128 KiB are neither
-- matched nor returned. The caller requests one extra row to detect more pages.
SELECT m.id, m.chat_id, m.seq, m.created_at, m.role, m.generation,
       COALESCE(CASE WHEN octet_length(m.source) <= 512 THEN m.source END, '')::text AS source,
       COALESCE(CASE WHEN octet_length(m.user_id) <= 512 THEN m.user_id END, '')::text AS user_id,
       COALESCE(CASE WHEN octet_length(c.title) <= 4096 THEN c.title END, '')::text AS chat_title,
       c.title_manually_set AS chat_title_manually_set,
       COALESCE(c.user_id, '')::text AS owner_user_id,
       CASE WHEN octet_length(m.content) <= 131072 THEN m.content ELSE '' END::text AS content,
       CASE WHEN octet_length(m.tool_calls::text) <= 131072 THEN m.tool_calls END::jsonb AS tool_calls,
       CASE WHEN octet_length(m.content_raw::text) <= 131072 THEN m.content_raw END::jsonb AS content_raw,
       (octet_length(m.content) > 131072 OR COALESCE(octet_length(m.tool_calls::text), 0) > 131072
        OR COALESCE(octet_length(m.content_raw::text), 0) > 131072)::boolean AS oversized,
       (COALESCE(octet_length(m.content), 0) > 131072)::boolean AS content_oversized,
       (COALESCE(octet_length(m.tool_calls::text), 0) > 131072)::boolean AS tool_calls_oversized,
       (COALESCE(octet_length(m.content_raw::text), 0) > 131072)::boolean AS content_raw_oversized,
       t.ciphertext AS shadow_ciphertext,
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.project_id = @project_id::uuid
  AND c.organization_id = @organization_id AND p.organization_id = @organization_id
  AND c.deleted IS FALSE AND p.deleted IS FALSE
  AND m.created_at >= @from_time AND m.created_at < @to_time
  AND (cardinality(@chat_ids::uuid[]) = 0 OR m.chat_id = ANY(@chat_ids::uuid[]))
  -- Own-scope visibility for a member without chat:read, the predicate the chat
  -- ListChats query applies: their sessions plus agent-only sessions of agents
  -- they own. Every other filter only narrows it further.
  AND (@own_user_id::text = '' OR c.user_id = @own_user_id
       OR (c.user_id IS NULL AND COALESCE(c.external_user_id, '') = '' AND c.agent_id = ANY(@owned_agent_ids::uuid[])))
  AND (@owner_user_id::text = '' OR c.user_id = @owner_user_id)
  AND (@source::text = '' OR m.source = @source)
  AND (@pattern::text = ''
       OR (octet_length(m.content) <= 131072 AND m.content ILIKE @pattern ESCAPE '\')
       OR (octet_length(m.tool_calls::text) <= 131072 AND m.tool_calls::text ILIKE @pattern ESCAPE '\')
       OR (octet_length(m.content_raw::text) <= 131072 AND m.content_raw::text ILIKE @pattern ESCAPE '\')
       OR c.title ILIKE @pattern ESCAPE '\'
       -- A stored cron envelope is shown as "Cron: <job name>" (chattitle.Display),
       -- so a search for the shown title matches it too.
       OR (NOT c.title_manually_set
           AND ('Cron: ' || substring(c.title from '^\s*\[cron:[0-9a-fA-F-]{36} ([^\]]+)')) ILIKE @pattern ESCAPE '\'))
  AND (@participant_id::text = ''
       OR (@participant_provider::text = '' AND m.user_id = @participant_id)
       OR EXISTS (
         SELECT 1 FROM chat_message_participants cp
         WHERE cp.project_id = m.project_id AND cp.chat_id = m.chat_id AND cp.message_id = m.id
           AND (cp.user_id = @participant_id OR cp.provider_user_id = @participant_id)
           AND (@participant_provider = '' OR cp.provider = @participant_provider)))
  AND (NOT @has_cursor::boolean
       OR (@oldest_first::boolean AND (m.created_at, m.id) > (@cursor_at::timestamptz, @cursor_id::uuid))
       OR (NOT @oldest_first AND (m.created_at, m.id) < (@cursor_at, @cursor_id)))
ORDER BY CASE WHEN @oldest_first THEN m.created_at END ASC,
         CASE WHEN @oldest_first THEN m.id END ASC,
         CASE WHEN NOT @oldest_first THEN m.created_at END DESC,
         CASE WHEN NOT @oldest_first THEN m.id END DESC
LIMIT @row_limit::integer;

-- name: ScanPlatformChatMessages :many
-- One chunk of the Platform MCP chat search in a project that keeps shadows:
-- the filters, order and cursor of SearchPlatformChatMessages without its text
-- match, which the store applies in Go to each opened shadow. title_match
-- reports whether the chat title (not transcript body) matches @pattern. A
-- message with a shadow returns its ciphertext and no plaintext body; one with
-- no shadow returns its plaintext, the fallback every read applies.
SELECT m.id, m.chat_id, m.seq, m.created_at, m.role, m.generation,
       COALESCE(CASE WHEN octet_length(m.source) <= 512 THEN m.source END, '')::text AS source,
       COALESCE(CASE WHEN octet_length(m.user_id) <= 512 THEN m.user_id END, '')::text AS user_id,
       COALESCE(CASE WHEN octet_length(c.title) <= 4096 THEN c.title END, '')::text AS chat_title,
       c.title_manually_set AS chat_title_manually_set,
       COALESCE(c.user_id, '')::text AS owner_user_id,
       COALESCE(c.title ILIKE @pattern::text ESCAPE '\'
         OR (NOT c.title_manually_set
             AND ('Cron: ' || substring(c.title from '^\s*\[cron:[0-9a-fA-F-]{36} ([^\]]+)')) ILIKE @pattern ESCAPE '\'), false)::boolean AS title_match,
       t.ciphertext AS shadow_ciphertext,
       COALESCE(CASE WHEN t.message_id IS NULL THEN m.content END, '')::text AS fallback_content,
       CASE WHEN t.message_id IS NULL THEN m.tool_calls END::jsonb AS fallback_tool_calls,
       CASE WHEN t.message_id IS NULL THEN m.content_raw END::jsonb AS fallback_content_raw
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = c.project_id
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id
WHERE m.project_id = @project_id::uuid
  AND c.organization_id = @organization_id AND p.organization_id = @organization_id
  AND c.deleted IS FALSE AND p.deleted IS FALSE
  AND m.created_at >= @from_time AND m.created_at < @to_time
  AND (cardinality(@chat_ids::uuid[]) = 0 OR m.chat_id = ANY(@chat_ids::uuid[]))
  AND (@own_user_id::text = '' OR c.user_id = @own_user_id
       OR (c.user_id IS NULL AND COALESCE(c.external_user_id, '') = '' AND c.agent_id = ANY(@owned_agent_ids::uuid[])))
  AND (@owner_user_id::text = '' OR c.user_id = @owner_user_id)
  AND (@source::text = '' OR m.source = @source)
  AND (@participant_id::text = ''
       OR (@participant_provider::text = '' AND m.user_id = @participant_id)
       OR EXISTS (
         SELECT 1 FROM chat_message_participants cp
         WHERE cp.project_id = m.project_id AND cp.chat_id = m.chat_id AND cp.message_id = m.id
           AND (cp.user_id = @participant_id OR cp.provider_user_id = @participant_id)
           AND (@participant_provider = '' OR cp.provider = @participant_provider)))
  AND (NOT @has_cursor::boolean
       OR (@oldest_first::boolean AND (m.created_at, m.id) > (@cursor_at::timestamptz, @cursor_id::uuid))
       OR (NOT @oldest_first AND (m.created_at, m.id) < (@cursor_at, @cursor_id)))
ORDER BY CASE WHEN @oldest_first THEN m.created_at END ASC,
         CASE WHEN @oldest_first THEN m.id END ASC,
         CASE WHEN NOT @oldest_first THEN m.created_at END DESC,
         CASE WHEN NOT @oldest_first THEN m.id END DESC
LIMIT @row_limit::integer;

-- name: ListSearchHitFields :many
-- The plaintext fields of the hits a chunked Platform MCP search matched on
-- transcripts it opened without reading plaintext. In shadow mode the store
-- compares each hit with its plaintext row, which stays the source of truth.
SELECT m.id, m.content, m.tool_calls, m.content_raw
FROM chat_messages m
WHERE m.project_id = @project_id::uuid
  AND m.id = ANY(@message_ids::uuid[]);

-- name: ReadPlatformChatMessages :many
-- Capture-sequence window around one message of this chat, or its latest one,
-- with up to five recorded participants per message.
WITH anchor AS (
    SELECT cm.seq FROM chat_messages cm
    JOIN chats c ON c.id = cm.chat_id AND c.project_id = cm.project_id AND c.deleted IS FALSE
    JOIN projects p ON p.id = c.project_id AND p.organization_id = c.organization_id AND p.deleted IS FALSE
    WHERE c.organization_id = @organization_id AND p.organization_id = @organization_id
      AND (sqlc.narg('message_id')::uuid IS NULL OR cm.id = sqlc.narg('message_id'))
      AND cm.chat_id = @chat_id AND cm.project_id = @project_id
    ORDER BY cm.seq DESC LIMIT 1
), preceding AS (
    SELECT cm.* FROM chat_messages cm, anchor a
     WHERE cm.chat_id = @chat_id AND cm.project_id = @project_id AND cm.seq < a.seq
     ORDER BY cm.seq DESC LIMIT @context_size::integer
), following AS (
    SELECT cm.* FROM chat_messages cm, anchor a
     WHERE cm.chat_id = @chat_id AND cm.project_id = @project_id AND cm.seq >= a.seq
     ORDER BY cm.seq ASC LIMIT @following_limit::integer
), neighbors AS (SELECT * FROM preceding UNION ALL SELECT * FROM following)
SELECT n.id, n.chat_id, n.seq, n.created_at, n.role, n.generation,
       COALESCE(CASE WHEN octet_length(n.source) <= 512 THEN n.source END, '')::text AS source,
       COALESCE(CASE WHEN octet_length(n.user_id) <= 512 THEN n.user_id END, '')::text AS user_id,
       CASE WHEN octet_length(n.content) <= 131072 THEN n.content ELSE '' END::text AS content,
       CASE WHEN octet_length(n.tool_calls::text) <= 131072 THEN n.tool_calls END::jsonb AS tool_calls,
       CASE WHEN octet_length(n.content_raw::text) <= 131072 THEN n.content_raw END::jsonb AS content_raw,
       (octet_length(n.content) > 131072 OR COALESCE(octet_length(n.tool_calls::text), 0) > 131072
        OR COALESCE(octet_length(n.content_raw::text), 0) > 131072)::boolean AS oversized,
       (COALESCE(octet_length(n.content), 0) > 131072)::boolean AS content_oversized,
       (COALESCE(octet_length(n.tool_calls::text), 0) > 131072)::boolean AS tool_calls_oversized,
       (COALESCE(octet_length(n.content_raw::text), 0) > 131072)::boolean AS content_raw_oversized,
       COALESCE((
         SELECT jsonb_agg(jsonb_build_object(
                  'provider', CASE WHEN octet_length(cp.provider) <= 512 THEN cp.provider END, 'provider_user_id', CASE WHEN octet_length(cp.provider_user_id) <= 512 THEN cp.provider_user_id END,
                  'user_id', COALESCE(CASE WHEN octet_length(cp.user_id) <= 512 THEN cp.user_id END, ''), 'display_name', COALESCE(CASE WHEN octet_length(cp.display_name) <= 1024 THEN cp.display_name END, '')))
         FROM (SELECT * FROM chat_message_participants cp
               WHERE cp.project_id = @project_id AND cp.chat_id = n.chat_id AND cp.message_id = n.id
               ORDER BY cp.provider, cp.provider_user_id, cp.id LIMIT 5) cp
       ), '[]')::jsonb AS participants,
       t.ciphertext AS shadow_ciphertext,
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM neighbors n
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = n.id AND t.project_id = n.project_id AND @open_shadows::boolean
ORDER BY n.seq;

-- name: ListOwnedChatTranscriptMessagesForRecall :many
-- Latest generation only: compaction/edit rewrites bump chat_messages.generation
-- and the digest must reflect the current conversation view, not superseded
-- rows. chat_messages.project_id is NULL on old rows — always filtered, so
-- pre-project-stamp rows fail closed rather than leaking across tenants.
-- Newest rows first under @row_limit so the cap keeps the end of a long
-- session — the part a handoff digest is about — and the service restores
-- chronological order. Content is never truncated here: finding-span
-- verification compares exact bytes, so per-message bounding happens after
-- masking, not at the read.
SELECT cm.id, cm.seq, cm.created_at, cm.role, cm.content, cm.content_asset_url, cm.tool_calls, cm.tool_call_id, cm.tool_urn, cm.source, cm.risk_analyzed_at,
       t.ciphertext AS shadow_ciphertext,
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages cm
JOIN chats c ON c.id = cm.chat_id
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = cm.id AND t.project_id = cm.project_id AND @open_shadows::boolean
LEFT JOIN user_accounts ua ON ua.id = c.user_account_id
WHERE cm.chat_id = @chat_id
  AND cm.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.user_id = @user_id::text
  AND c.deleted IS FALSE
  AND (ua.id IS NULL OR ua.account_type <> 'personal')
  AND cm.generation = (
    SELECT COALESCE(MAX(generation), 0)
    FROM chat_messages
    WHERE chat_id = @chat_id
      AND project_id = @project_id
  )
ORDER BY cm.created_at DESC, cm.seq DESC
LIMIT @row_limit;

-- name: LoadEvaluationMessages :many
-- One tenant-pinned read resolves the batch and its current attachment locators.
-- System messages are excluded for every input strategy.
SELECT m.id, m.chat_id, m.project_id, m.seq, m.generation, m.created_at,
       m.role, m.content, m.content_raw, m.content_asset_url, m.tool_calls,
       m.user_id, m.external_user_id, c.user_account_id,
       COALESCE(m.origin = 'anthropic-inference' AND (
         m.external_message_id LIKE '%/block:%' OR EXISTS (
           SELECT 1 FROM chat_messages sibling
           WHERE sibling.project_id = m.project_id AND sibling.chat_id = m.chat_id
             AND sibling.role <> 'system'
             AND sibling.external_message_id = m.external_message_id || '/block:0'
         )
       ), false)::boolean AS row_local_content,
       ARRAY(SELECT cp.content_asset_url FROM chat_content_parts cp
             WHERE cp.project_id = m.project_id AND cp.chat_id = m.chat_id
               AND cp.parent_chat_message_id = m.id AND cp.deleted IS FALSE
             ORDER BY cp.created_at, cp.id)::text[] AS attachment_uris,
       t.ciphertext AS shadow_ciphertext,
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = m.project_id
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
WHERE m.project_id = @project_id::uuid AND p.organization_id = @organization_id
  AND p.deleted IS FALSE AND c.deleted IS FALSE
  AND m.role <> 'system'
  AND m.id = ANY(@message_ids::uuid[]);

-- name: LoadEvaluationHistory :many
-- Anchor to the target snapshot, never to the latest message in the conversation.
-- Filter system messages before LIMIT so they do not consume window capacity.
-- Bound the cumulative inline payload before transferring it to the application.
-- Keep overflow rows as markers: skipping them would change window membership.
-- Reserve eight bytes per attachment locator for array element framing.
WITH candidates AS MATERIALIZED (
    SELECT m.id, m.project_id, m.chat_id, m.created_at, m.seq,
           m.role, m.content, m.content_raw, m.content_asset_url, m.tool_calls,
           m.origin, m.external_message_id
    FROM chat_messages m
    JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
    JOIN projects p ON p.id = m.project_id
    WHERE m.project_id = @project_id::uuid AND p.organization_id = @organization_id
      AND p.deleted IS FALSE AND c.deleted IS FALSE
      AND m.role <> 'system'
      AND m.chat_id = @chat_id AND m.generation = @generation
      AND m.id <> @target_id
      AND (m.created_at, m.seq) < (@target_created_at::timestamptz, @target_seq::bigint)
    ORDER BY m.created_at DESC, m.seq DESC
    LIMIT @history_limit
), sized AS (
    SELECT m.*,
           (octet_length(m.role)::bigint + octet_length(m.content)::bigint
            + COALESCE(octet_length(m.content_raw::text), 0)::bigint
            + COALESCE(octet_length(m.content_asset_url), 0)::bigint
            + COALESCE(octet_length(m.tool_calls::text), 0)::bigint
            + COALESCE((SELECT sum(octet_length(cp.content_asset_url)::bigint + 8)
                        FROM chat_content_parts cp
                        WHERE cp.project_id = m.project_id AND cp.chat_id = m.chat_id
                          AND cp.parent_chat_message_id = m.id AND cp.deleted IS FALSE), 0)
           ) AS payload_bytes
    FROM candidates m
), bounded AS (
    SELECT m.*,
           (sum(payload_bytes) OVER (ORDER BY created_at DESC, seq DESC ROWS UNBOUNDED PRECEDING)
            > @content_budget::bigint)::boolean AS content_too_large
    FROM sized m
)
SELECT m.id,
       (CASE WHEN m.content_too_large THEN '' ELSE m.role END)::text AS role,
       (CASE WHEN m.content_too_large THEN '' ELSE m.content END)::text AS content,
       (CASE WHEN m.content_too_large THEN NULL ELSE m.content_raw END)::jsonb AS content_raw,
       (CASE WHEN m.content_too_large THEN '' ELSE COALESCE(m.content_asset_url, '') END)::text AS content_asset_url,
       (CASE WHEN m.content_too_large THEN NULL ELSE m.tool_calls END)::jsonb AS tool_calls,
       COALESCE(m.origin = 'anthropic-inference' AND (
         m.external_message_id LIKE '%/block:%' OR EXISTS (
           SELECT 1 FROM chat_messages sibling
           WHERE sibling.project_id = m.project_id AND sibling.chat_id = m.chat_id
             AND sibling.role <> 'system'
             AND sibling.external_message_id = m.external_message_id || '/block:0'
         )
       ), false)::boolean AS row_local_content,
        CASE WHEN m.content_too_large THEN ARRAY[]::text[] ELSE
        ARRAY(SELECT cp.content_asset_url FROM chat_content_parts cp
             WHERE cp.project_id = m.project_id AND cp.chat_id = m.chat_id
               AND cp.parent_chat_message_id = m.id AND cp.deleted IS FALSE
              ORDER BY cp.created_at, cp.id)::text[] END AS attachment_uris,
        m.content_too_large,
        t.ciphertext AS shadow_ciphertext,
       (t.ciphertext IS NOT NULL)::boolean AS shadow_current
FROM bounded m
LEFT JOIN chat_message_transcripts t ON t.source_hash IS NULL AND t.message_id = m.id AND t.project_id = m.project_id AND @open_shadows::boolean
LEFT JOIN chat_messages src ON src.id = t.message_id
ORDER BY m.created_at DESC, m.seq DESC;

-- name: FindAssistantToolCallMessageID :one
SELECT id
FROM chat_messages
WHERE project_id = sqlc.arg(project_id)
  AND chat_id = sqlc.arg(chat_id)
  AND role = 'assistant'
  AND tool_calls IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM jsonb_array_elements(tool_calls) tc
    WHERE tc->>'id' = sqlc.arg(tool_call_id)::text
  )
ORDER BY created_at DESC
LIMIT 1;

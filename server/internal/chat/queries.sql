-- name: GetAssistantThreadAssistantIDByChatID :one
SELECT t.assistant_id
FROM assistant_threads t
WHERE t.chat_id = @chat_id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
LIMIT 1;

-- name: ChatBacksLiveAssistantThread :one
-- True when the chat backs a thread of a live (non-deleted) assistant in the
-- given project. Project-scoped so a chat ID from another project falls through
-- to the project-scoped SoftDeleteChat no-op rather than leaking existence via a
-- 409. The assistant join matters because DeleteAssistant only soft-deletes the
-- assistant, leaving its threads behind — those orphaned threads must not keep
-- the backing chat undeletable forever.
SELECT EXISTS (
  SELECT 1
  FROM assistant_threads t
  JOIN assistants a ON a.id = t.assistant_id
  WHERE t.chat_id = @chat_id
    AND t.project_id = @project_id
    AND t.deleted IS FALSE
    AND a.deleted IS FALSE
) AS backs_thread;

-- name: UpsertChat :one
INSERT INTO chats (
    id
  , project_id
  , organization_id
  , user_id
  , external_user_id
  , title
  , created_at
  , updated_at
)
VALUES (
    @id,
    @project_id,
    @organization_id,
    @user_id,
    @external_user_id,
    @title,
    NOW(),
    NOW()
)
-- On conflict, self-heal a soft-deleted chat that still backs a live assistant
-- thread: the runtime keeps writing to it, so it must not stay marked deleted (a
-- deleted chat wedges the thread — compaction-persist 404s on it). Scoped to
-- thread-backed chats so a write does NOT resurrect a plain chat a user
-- intentionally deleted. The first WHEN keeps the common (non-deleted) path a
-- no-op without touching assistant_threads, so the EXISTS stays off the
-- /chat/completions hot path and only runs for the rare already-deleted row. The
-- assistant join means a deleted assistant's leftover thread can't heal the
-- chat. The SET also guarantees RETURNING yields a row whether the chat was
-- newly inserted or already existed.
ON CONFLICT (id) DO UPDATE SET deleted_at = CASE
    WHEN chats.deleted_at IS NULL THEN NULL
    WHEN chats.project_id = EXCLUDED.project_id AND EXISTS (
        SELECT 1 FROM assistant_threads t
        JOIN assistants a ON a.id = t.assistant_id
        WHERE t.chat_id = chats.id AND t.project_id = chats.project_id
          AND t.deleted IS FALSE AND a.deleted IS FALSE
    ) THEN NULL
    ELSE chats.deleted_at
END
RETURNING id;

-- name: UpsertExternalChat :one
INSERT INTO chats (
    id
  , project_id
  , organization_id
  , user_id
  , external_user_id
  , external_chat_id
  , title
  , created_at
  , updated_at
)
VALUES (
    @id
  , @project_id
  , @organization_id
  , @user_id
  , @external_user_id
  , @external_chat_id
  , @title
  , @created_at
  , @updated_at
)
ON CONFLICT (organization_id, external_chat_id) WHERE external_chat_id IS NOT NULL
DO UPDATE SET
    project_id = EXCLUDED.project_id
  , user_id = COALESCE(EXCLUDED.user_id, chats.user_id)
  , external_user_id = COALESCE(EXCLUDED.external_user_id, chats.external_user_id)
  , title = COALESCE(EXCLUDED.title, chats.title)
  , updated_at = GREATEST(chats.updated_at, EXCLUDED.updated_at)
RETURNING id;

-- name: LinkAIIntegrationConfigChat :one
-- Links a chat to the AI integration config that imported it and returns the
-- chat's persisted message pagination cursor so imports resume where the last
-- successful page ended.
INSERT INTO ai_integration_config_chats (
    ai_integration_config_id
  , chat_id
)
VALUES (
    @ai_integration_config_id
  , @chat_id
)
ON CONFLICT (chat_id)
DO UPDATE SET
    ai_integration_config_id = EXCLUDED.ai_integration_config_id
  , updated_at = clock_timestamp()
RETURNING last_cursor_id;

-- name: UpdateAIIntegrationConfigChatCursor :exec
UPDATE ai_integration_config_chats
SET last_cursor_id = @last_cursor_id
  , updated_at = clock_timestamp()
WHERE chat_id = @chat_id;

-- name: CreateChatMessage :copyfrom
INSERT INTO chat_messages (
    chat_id
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
)
VALUES (
    @chat_id
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
);

-- name: CreateExternalChatMessage :execrows
INSERT INTO chat_messages (
    chat_id
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
  , created_at
)
VALUES (
    @chat_id
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
  , @created_at
)
ON CONFLICT (chat_id, external_message_id) WHERE external_message_id IS NOT NULL
DO NOTHING;

-- name: CountChats :one
WITH candidate_chats AS (
  SELECT c.id, c.created_at
  FROM chats c
  WHERE c.project_id = @project_id
    AND c.deleted IS FALSE
    AND (@external_user_id = '' OR c.external_user_id = @external_user_id)
    AND (@user_id = '' OR c.user_id = @user_id)
    AND (
      @search = ''
      OR c.id::text ILIKE '%' || @search || '%'
      OR c.external_user_id ILIKE '%' || @search || '%'
      OR c.title ILIKE '%' || @search || '%'
    )
    AND (
      @assistant_id = ''
      OR EXISTS (
        SELECT 1 FROM assistant_threads at
        WHERE at.chat_id = c.id
          AND at.assistant_id = @assistant_id::uuid
          AND at.deleted IS FALSE
      )
    )
    AND (
      @has_risk_filter::text = ''
      OR (
        @has_risk_filter::text = 'true' AND EXISTS (
          SELECT 1 FROM risk_results rr
          JOIN chat_messages cm ON cm.id = rr.chat_message_id
          WHERE cm.chat_id = c.id
            AND rr.project_id = @project_id
            AND rr.found IS TRUE
            AND rr.excluded_at IS NULL
            AND rr.false_positive_at IS NULL
        )
      )
      OR (
        @has_risk_filter::text = 'false' AND NOT EXISTS (
          SELECT 1 FROM risk_results rr
          JOIN chat_messages cm ON cm.id = rr.chat_message_id
          WHERE cm.chat_id = c.id
            AND rr.project_id = @project_id
            AND rr.found IS TRUE
            AND rr.excluded_at IS NULL
            AND rr.false_positive_at IS NULL
        )
      )
    )
),
chat_activity AS (
  SELECT
    cc.id,
    COALESCE(MAX(cm.created_at), cc.created_at) AS last_message_timestamp
  FROM candidate_chats cc
  LEFT JOIN chat_messages cm ON cm.chat_id = cc.id
  GROUP BY cc.id, cc.created_at
)
SELECT COUNT(*) AS total
FROM chat_activity ca
WHERE (@from_time::timestamptz IS NULL OR ca.last_message_timestamp >= @from_time)
  AND (@to_time::timestamptz IS NULL OR ca.last_message_timestamp <= @to_time);

-- name: ListChats :many
WITH candidate_chats AS (
  SELECT
    c.id,
    c.title,
    c.user_id,
    c.external_user_id,
    c.created_at,
    c.updated_at
  FROM chats c
  WHERE c.project_id = @project_id
    AND c.deleted IS FALSE
    AND (@external_user_id = '' OR c.external_user_id = @external_user_id)
    AND (@user_id = '' OR c.user_id = @user_id)
    AND (
      @search = ''
      OR c.id::text ILIKE '%' || @search || '%'
      OR c.external_user_id ILIKE '%' || @search || '%'
      OR c.title ILIKE '%' || @search || '%'
    )
    AND (
      @assistant_id = ''
      OR EXISTS (
        SELECT 1 FROM assistant_threads at
        WHERE at.chat_id = c.id
          AND at.assistant_id = @assistant_id::uuid
          AND at.deleted IS FALSE
      )
    )
    AND (
      @has_risk_filter::text = ''
      OR (
        @has_risk_filter::text = 'true' AND EXISTS (
          SELECT 1 FROM risk_results rr
          JOIN chat_messages cm ON cm.id = rr.chat_message_id
          WHERE cm.chat_id = c.id
            AND rr.project_id = @project_id
            AND rr.found IS TRUE
            AND rr.excluded_at IS NULL
            AND rr.false_positive_at IS NULL
        )
      )
      OR (
        @has_risk_filter::text = 'false' AND NOT EXISTS (
          SELECT 1 FROM risk_results rr
          JOIN chat_messages cm ON cm.id = rr.chat_message_id
          WHERE cm.chat_id = c.id
            AND rr.project_id = @project_id
            AND rr.found IS TRUE
            AND rr.excluded_at IS NULL
            AND rr.false_positive_at IS NULL
        )
      )
    )
),
chat_stats AS (
  SELECT
    cc.id,
    COUNT(cm.id)::integer AS num_messages,
    COALESCE(MAX(cm.created_at), cc.created_at) AS last_message_timestamp
  FROM candidate_chats cc
  LEFT JOIN chat_messages cm ON cm.chat_id = cc.id
  GROUP BY cc.id, cc.created_at
),
filtered_chats AS (
  SELECT
    cc.id,
    cc.title,
    cc.user_id,
    cc.external_user_id,
    cc.created_at,
    cc.updated_at,
    cs.num_messages,
    cs.last_message_timestamp,
    (
      SELECT COUNT(*)::integer
      FROM risk_results rr
      JOIN chat_messages cm ON cm.id = rr.chat_message_id
      WHERE cm.chat_id = cc.id
        AND rr.project_id = @project_id
        AND rr.found IS TRUE
        AND rr.excluded_at IS NULL
        AND rr.false_positive_at IS NULL
    ) AS risk_findings_count
  FROM candidate_chats cc
  JOIN chat_stats cs ON cs.id = cc.id
  WHERE (@from_time::timestamptz IS NULL OR cs.last_message_timestamp >= @from_time)
    AND (@to_time::timestamptz IS NULL OR cs.last_message_timestamp <= @to_time)
),
limited_chats AS (
  SELECT
    fc.id,
    fc.title,
    fc.user_id,
    fc.external_user_id,
    fc.created_at,
    fc.updated_at,
    fc.num_messages,
    (SELECT source FROM chat_messages WHERE chat_id = fc.id AND source IS NOT NULL ORDER BY created_at DESC LIMIT 1) AS source,
    fc.last_message_timestamp,
    fc.risk_findings_count
  FROM filtered_chats fc
  ORDER BY
    CASE WHEN @sort_by = 'last_message_timestamp' AND @sort_order = 'desc' THEN fc.last_message_timestamp END DESC NULLS LAST,
    CASE WHEN @sort_by = 'last_message_timestamp' AND @sort_order = 'asc' THEN fc.last_message_timestamp END ASC NULLS LAST,
    CASE WHEN @sort_by = 'num_messages' AND @sort_order = 'desc' THEN fc.num_messages END DESC NULLS LAST,
    CASE WHEN @sort_by = 'num_messages' AND @sort_order = 'asc' THEN fc.num_messages END ASC NULLS LAST,
    fc.last_message_timestamp DESC,
    fc.id DESC
  LIMIT @page_limit
  OFFSET @page_offset
)
SELECT
  lc.id,
  lc.title,
  lc.user_id,
  lc.external_user_id,
  lc.source,
  lc.created_at,
  lc.updated_at,
  lc.num_messages,
  lc.last_message_timestamp,
  lc.risk_findings_count
FROM limited_chats lc;

-- name: GetChat :one
SELECT * FROM chats WHERE id = @id AND deleted IS FALSE;

-- name: ListChatMessages :many
SELECT * FROM chat_messages 
WHERE chat_id = @chat_id AND (project_id IS NULL OR project_id = @project_id::uuid) 
ORDER BY seq ASC;

-- name: CountChatMessages :one
-- Must match ListChatMessages' project_id filter, otherwise count and the
-- list drift and the client hits "chat history mismatch" at
-- message_capture_strategy.go.
SELECT COUNT(*) FROM chat_messages
WHERE chat_id = @chat_id AND (project_id IS NULL OR project_id = @project_id::uuid);

-- name: GetChatMessageStats :one
-- Chat-wide aggregates (total message count + most recent message timestamp).
-- Used by loadChat so every paginated response can carry the chat's real
-- last_message_timestamp without depending on chats.updated_at, which is bumped
-- by metadata edits (e.g. title changes) and would drift from the actual last
-- message.
SELECT
  COUNT(*)::bigint AS total,
  MAX(created_at)::timestamptz AS last_message_at
FROM chat_messages
WHERE chat_id = @chat_id AND (project_id IS NULL OR project_id = @project_id::uuid);

-- name: GetChatEntryTotals :one
-- Per-generation trace-entry totals for the chat detail filter bar. The detail
-- sheet paginates messages, so counts derived from the loaded page understate
-- the chat; these totals describe the whole generation regardless of which page
-- is in view. Each message maps to exactly one entry, mirroring the client's
-- getTraceEntryType precedence: a message carrying a non-empty tool_calls array
-- is a tool call regardless of role, otherwise the role decides. risk_findings
-- counts messages with an active (found, non-suppressed) risk result.
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
    AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
    AND cm.generation = @generation::integer
)
SELECT
  COUNT(*) FILTER (WHERE has_tool_calls OR role IN ('user', 'assistant', 'tool'))::bigint AS total,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'user')::bigint AS user_messages,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'assistant')::bigint AS assistant_messages,
  COUNT(*) FILTER (WHERE has_tool_calls)::bigint AS tool_calls,
  COUNT(*) FILTER (WHERE NOT has_tool_calls AND role = 'tool')::bigint AS tool_results,
  (
    SELECT COUNT(*)::bigint FROM ordered o
    WHERE EXISTS (
      SELECT 1 FROM risk_results rr
      WHERE rr.chat_message_id = o.id
        AND rr.project_id = @project_id::uuid
        AND rr.found IS TRUE
        AND rr.excluded_at IS NULL
        AND rr.false_positive_at IS NULL
    )
  )::bigint AS risk_findings
FROM ordered;

-- name: ListLatestGenerationChatMessages :many
-- Returns only the latest-generation rows; older generations are audit-only.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = (SELECT MAX(generation) FROM chat_messages WHERE chat_id = @chat_id)
ORDER BY cm.seq ASC;

-- name: ListChatMessagesByGeneration :many
-- Returns rows for an explicit generation, used to pin a snapshot across
-- multiple reads (e.g. across Temporal activities) so indices stay stable
-- even if a new generation is appended mid-workflow.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = @generation::integer
ORDER BY cm.seq ASC;

-- name: GetMaxGenerationForChat :one
SELECT COALESCE(MAX(generation), 0)::integer AS generation FROM chat_messages WHERE chat_id = @chat_id;

-- name: ListChatMessagesBeforePage :many
-- Keyset page within a generation, newest first. Returns messages with seq
-- strictly less than @before_seq, or the newest page when @before_seq is NULL.
-- Order DESC so LIMIT keeps the most recent rows; the caller reverses to
-- ascending for display. Fetch @lim = pageSize+1 to detect whether more older
-- rows remain.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = @generation::integer
  AND (sqlc.narg('before_seq')::bigint IS NULL OR cm.seq < sqlc.narg('before_seq')::bigint)
ORDER BY cm.seq DESC
LIMIT @lim::integer;

-- name: ListChatMessagesAfterPage :many
-- Keyset page within a generation, oldest first. Returns messages with seq
-- strictly greater than @after_seq. Fetch @lim = pageSize+1 to detect whether
-- more newer rows remain.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = @generation::integer
  AND cm.seq > @after_seq::bigint
ORDER BY cm.seq ASC
LIMIT @lim::integer;

-- name: ListRiskWindowedMessages :many
-- Risk-only view: returns every message within +/- @context_size ordinal
-- positions of any active risk finding in the generation, ordered oldest to
-- newest. rn is the message's 1-based ordinal within the generation and total
-- is the generation's message count, so the caller can fold consecutive rn into
-- contiguous segments and decide whether earlier (rn > 1) or later (rn < total)
-- messages remain to be expanded. Overlapping windows merge naturally via set
-- membership.
WITH ordered AS (
  SELECT
    cm.*,
    row_number() OVER (ORDER BY cm.seq) AS rn,
    count(*) OVER () AS total
  FROM chat_messages cm
  WHERE cm.chat_id = @chat_id
    AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
    AND cm.generation = @generation::integer
),
risk_rns AS (
  SELECT o.rn FROM ordered o
  WHERE EXISTS (
    SELECT 1 FROM risk_results rr
    WHERE rr.chat_message_id = o.id
      AND rr.project_id = @project_id::uuid
      AND rr.found IS TRUE
      AND rr.excluded_at IS NULL
      AND rr.false_positive_at IS NULL
  )
)
SELECT o.*
FROM ordered o
WHERE EXISTS (
  SELECT 1 FROM risk_rns r
  WHERE o.rn BETWEEN r.rn - @context_size::bigint AND r.rn + @context_size::bigint
)
ORDER BY o.seq ASC;

-- name: ListChatMessagesForMatch :many
SELECT id, role, content, tool_call_id, tool_calls
FROM chat_messages
WHERE chat_id = @chat_id AND generation = @generation
ORDER BY seq ASC;

-- name: UpdateChatTitle :exec
UPDATE chats SET title = @title, updated_at = NOW() WHERE id = @id;

-- name: GetFirstUserChatMessage :one
SELECT content FROM chat_messages
WHERE chat_id = @chat_id
  AND role = 'user'
  AND content IS NOT NULL
  AND content != ''
ORDER BY created_at ASC
LIMIT 1;

-- name: GetToolCallMessages :many
SELECT * FROM chat_messages
WHERE chat_id = @chat_id
  AND role = 'tool'
ORDER BY created_at ASC;

-- name: UpdateToolCallOutcome :exec
UPDATE chat_messages
SET tool_outcome = @tool_outcome,
    tool_outcome_notes = @tool_outcome_notes
WHERE id = @id;

-- name: InsertChatResolution :one
INSERT INTO chat_resolutions (
    project_id,
    chat_id,
    user_goal,
    resolution,
    resolution_notes,
    score
) VALUES (
    @project_id,
    @chat_id,
    @user_goal,
    @resolution,
    @resolution_notes,
    @score
) RETURNING id;

-- name: InsertChatResolutionMessage :exec
INSERT INTO chat_resolution_messages (
    chat_resolution_id,
    message_id
) VALUES (
    @chat_resolution_id,
    @message_id
);

-- name: DeleteChatResolutions :exec
DELETE FROM chat_resolutions WHERE chat_id = @chat_id;

-- name: ListChatResolutions :many
SELECT * FROM chat_resolutions
WHERE chat_id = @chat_id
ORDER BY created_at DESC;

-- name: CountChatsWithResolutions :one
SELECT COUNT(DISTINCT c.id) as total
FROM chats c
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND (@external_user_id = '' OR c.external_user_id = @external_user_id)
  AND (@from_time::timestamptz IS NULL OR c.created_at >= @from_time)
  AND (@to_time::timestamptz IS NULL OR c.created_at <= @to_time)
  AND (
    @search = ''
    OR c.id::text ILIKE '%' || @search || '%'
    OR c.external_user_id ILIKE '%' || @search || '%'
    OR c.title ILIKE '%' || @search || '%'
  )
  AND (
    @assistant_id = ''
    OR EXISTS (
      SELECT 1 FROM assistant_threads at
      WHERE at.chat_id = c.id
        AND at.assistant_id = @assistant_id::uuid
        AND at.deleted IS FALSE
    )
  )
  AND (
    @resolution_status = ''
    OR (
      @resolution_status = 'unresolved' AND NOT EXISTS (
        SELECT 1 FROM chat_resolutions WHERE chat_id = c.id
      )
    )
    OR (
      @resolution_status != 'unresolved' AND EXISTS (
        SELECT 1 FROM chat_resolutions WHERE chat_id = c.id AND resolution = @resolution_status
      )
    )
  )
  AND (
    @has_risk_filter::text = ''
    OR (
      @has_risk_filter::text = 'true' AND EXISTS (
        SELECT 1 FROM risk_results rr
        JOIN chat_messages cm ON cm.id = rr.chat_message_id
        WHERE cm.chat_id = c.id
          AND rr.project_id = @project_id
          AND rr.found IS TRUE
          AND rr.excluded_at IS NULL
          AND rr.false_positive_at IS NULL
      )
    )
    OR (
      @has_risk_filter::text = 'false' AND NOT EXISTS (
        SELECT 1 FROM risk_results rr
        JOIN chat_messages cm ON cm.id = rr.chat_message_id
        WHERE cm.chat_id = c.id
          AND rr.project_id = @project_id
          AND rr.found IS TRUE
          AND rr.excluded_at IS NULL
          AND rr.false_positive_at IS NULL
      )
    )
  );

-- name: ListUserFeedbackForChat :many
SELECT *
FROM chat_user_feedback
WHERE chat_id = @chat_id
ORDER BY created_at DESC;

-- name: DeleteChatResolutionsAfterMessage :exec
DELETE FROM chat_resolutions
WHERE id IN (
    SELECT DISTINCT cr.id
    FROM chat_resolutions cr
    JOIN chat_resolution_messages crm ON cr.id = crm.chat_resolution_id
    JOIN chat_messages cm ON crm.message_id = cm.id
    WHERE cr.chat_id = @chat_id
      AND cm.seq > (
        SELECT seq FROM chat_messages WHERE chat_messages.id = @after_message_id
      )
  );

-- name: InsertUserFeedback :one
INSERT INTO chat_user_feedback (
    project_id,
    chat_id,
    message_id,
    user_resolution,
    user_resolution_notes,
    chat_resolution_id
) VALUES (
    @project_id,
    @chat_id,
    @message_id,
    @user_resolution,
    @user_resolution_notes,
    @chat_resolution_id
) RETURNING id;

-- name: AddUserFeedbackChatResolution :exec
UPDATE chat_user_feedback
SET chat_resolution_id = @chat_resolution_id
WHERE id = @id;

-- name: SoftDeleteChat :exec
UPDATE chats
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetTopUsersByMessages :many
SELECT
  COALESCE(NULLIF(c.external_user_id, ''), c.user_id) as user_id,
  CASE WHEN c.external_user_id IS NOT NULL AND c.external_user_id != '' THEN 'external' ELSE 'internal' END as user_type,
  COUNT(DISTINCT m.id) as message_count
FROM chats c
INNER JOIN chat_messages m ON m.chat_id = c.id
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND m.created_at >= @time_start
  AND m.created_at <= @time_end
  AND m.role IN ('user', 'assistant')
  AND COALESCE(NULLIF(c.external_user_id, ''), c.user_id) IS NOT NULL
GROUP BY COALESCE(NULLIF(c.external_user_id, ''), c.user_id), CASE WHEN c.external_user_id IS NOT NULL AND c.external_user_id != '' THEN 'external' ELSE 'internal' END
ORDER BY message_count DESC
LIMIT @result_limit;

-- name: GetLLMClientBreakdownByMessages :many
SELECT
  COALESCE(m.source, 'unknown') as client_name,
  COUNT(DISTINCT m.id) as message_count
FROM chats c
INNER JOIN chat_messages m ON m.chat_id = c.id
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND m.created_at >= @time_start
  AND m.created_at <= @time_end
  AND m.role IN ('user', 'assistant')
GROUP BY client_name
ORDER BY message_count DESC;

-- name: GetActiveUserCountByMessages :one
SELECT
  COUNT(DISTINCT COALESCE(NULLIF(c.external_user_id, ''), c.user_id))::bigint as active_user_count
FROM chats c
INNER JOIN chat_messages m ON m.chat_id = c.id
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND m.created_at >= @time_start
  AND m.created_at <= @time_end
  AND m.role IN ('user', 'assistant')
  AND COALESCE(NULLIF(c.external_user_id, ''), c.user_id) IS NOT NULL;

-- name: GetChatSessionCount :one
SELECT COUNT(DISTINCT c.id)::bigint as session_count
FROM chats c
INNER JOIN chat_messages m ON m.chat_id = c.id
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
  AND m.created_at >= @time_start
  AND m.created_at <= @time_end;

-- name: GetChatMetricsSummary :one
WITH chat_stats AS (
  SELECT
    c.id as chat_id,
    MIN(m.created_at) as first_message_at,
    MAX(m.created_at) as last_message_at,
    EXTRACT(EPOCH FROM (MAX(m.created_at) - MIN(m.created_at))) * 1000 as duration_ms,
    COALESCE(
      (SELECT resolution FROM chat_resolutions WHERE chat_id = c.id ORDER BY created_at DESC LIMIT 1),
      ''
    ) as resolution_status
  FROM chats c
  INNER JOIN chat_messages m ON m.chat_id = c.id
  WHERE c.project_id = @project_id
    AND c.deleted IS FALSE
    AND m.created_at >= @time_start
    AND m.created_at <= @time_end
  GROUP BY c.id
)
SELECT
  COUNT(*)::bigint as total_chats,
  COALESCE(SUM(CASE WHEN resolution_status = 'success' THEN 1 ELSE 0 END), 0)::bigint as resolved_chats,
  COALESCE(SUM(CASE WHEN resolution_status = 'failure' THEN 1 ELSE 0 END), 0)::bigint as failed_chats,
  COALESCE(AVG(duration_ms), 0)::double precision as avg_session_duration_ms,
  COALESCE(AVG(CASE WHEN resolution_status != '' THEN duration_ms END), 0)::double precision as avg_resolution_time_ms
FROM chat_stats;

-- name: CreateChatMessageWithToolCalls :exec
-- Inserts a single chat_messages row with optional tool_calls JSON,
-- tool_call_id, and generation, for callers seeding tool-turn history without
-- the full CreateChatMessage :copyfrom batch shape.
INSERT INTO chat_messages (chat_id, project_id, role, content, tool_calls, tool_call_id, generation)
VALUES (@chat_id, @project_id, @role, @content, @tool_calls, @tool_call_id, @generation);

-- name: SeedChatAtTime :one
-- Test fixture: insert a chat with a specific created_at for date range tests.
INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id, title, created_at, updated_at)
VALUES (@id, @project_id, @organization_id, @user_id, @external_user_id, @title, @created_at, @created_at)
ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.id
RETURNING id;

-- name: SeedChatMessage :one
-- Test fixture: insert a minimal chat message and return its id. An optional
-- created_at lets ordering tests assign distinct, deterministic timestamps
-- instead of relying on wall-clock gaps between inserts.
INSERT INTO chat_messages (chat_id, project_id, role, content, created_at)
VALUES (@chat_id, @project_id, 'user', 'test message', COALESCE(sqlc.narg('created_at')::timestamptz, clock_timestamp()))
RETURNING id;

-- name: SeedRiskPolicy :one
-- Test fixture: insert a minimal risk policy and return its id.
INSERT INTO risk_policies (project_id, organization_id, name, sources, enabled, action, auto_name, version)
VALUES (@project_id, @organization_id, 'test-policy', '{}', TRUE, 'flag', TRUE, 1)
RETURNING id;

-- name: SeedRiskResult :exec
-- Test fixture: insert a risk result linking a chat message to a risk policy.
INSERT INTO risk_results (
    project_id, organization_id, risk_policy_id, risk_policy_version,
    chat_message_id, source, found
)
VALUES (
    @project_id, @organization_id, @risk_policy_id, 1,
    @chat_message_id, 'test', @found
);

-- name: SeedAssistant :one
-- Test fixture: insert a minimal assistant and return its id.
INSERT INTO assistants (project_id, organization_id, name, model, instructions)
VALUES (@project_id, @organization_id, @name, 'anthropic/claude-opus-4.8', 'be helpful')
RETURNING id;

-- name: SeedAssistantThread :exec
-- Test fixture: insert an active assistant thread backed by a chat.
INSERT INTO assistant_threads (assistant_id, project_id, correlation_id, chat_id, source_kind)
VALUES (@assistant_id, @project_id, @correlation_id, @chat_id, 'cron');

-- name: SeedSoftDeleteAssistant :exec
-- Test fixture: soft-delete an assistant (mirrors DeleteAssistant, which leaves
-- its threads behind).
UPDATE assistants SET deleted_at = clock_timestamp() WHERE id = @id;

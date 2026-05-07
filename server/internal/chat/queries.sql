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
-- Use no-op update (id = EXCLUDED.id) to ensure RETURNING always returns a row,
-- whether the chat was newly inserted or already existed.
ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.id
RETURNING id;

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

-- name: ListAllChats :many
SELECT
    c.*,
    (
        COALESCE(
            (SELECT COUNT(*) FROM chat_messages WHERE chat_id = c.id),
            0
        )
    )::integer as num_messages
    , (
        COALESCE(
            (SELECT SUM(total_tokens) FROM chat_messages WHERE chat_id = c.id),
            0
        )
    )::integer as total_tokens
    , (SELECT source FROM chat_messages WHERE chat_id = c.id AND source IS NOT NULL ORDER BY created_at DESC LIMIT 1) as source
    , (SELECT created_at FROM chat_messages WHERE chat_id = c.id ORDER BY created_at DESC LIMIT 1) as last_message_timestamp
FROM chats c
WHERE c.project_id = @project_id
  AND c.deleted IS FALSE
ORDER BY c.updated_at DESC;

-- name: ListChatsForExternalUser :many
SELECT
    c.*,
    (
        COALESCE(
            (SELECT COUNT(*) FROM chat_messages WHERE chat_id = c.id),
            0
        )
    )::integer as num_messages
    , (
        COALESCE(
            (SELECT SUM(total_tokens) FROM chat_messages WHERE chat_id = c.id),
            0
        )
    )::integer as total_tokens
    , (SELECT source FROM chat_messages WHERE chat_id = c.id AND source IS NOT NULL ORDER BY created_at DESC LIMIT 1) as source
    , (SELECT created_at FROM chat_messages WHERE chat_id = c.id ORDER BY created_at DESC LIMIT 1) as last_message_timestamp
FROM chats c
WHERE c.project_id = @project_id AND c.external_user_id = @external_user_id
  AND c.deleted IS FALSE
ORDER BY c.updated_at DESC;


-- name: ListChatsForUser :many
SELECT
    c.*,
    (
        COALESCE(
            (SELECT COUNT(*) FROM chat_messages WHERE chat_id = c.id),
            0
        )
    )::integer as num_messages
    , (
        COALESCE(
            (SELECT SUM(total_tokens) FROM chat_messages WHERE chat_id = c.id),
            0
        )
    )::integer as total_tokens
    , (SELECT source FROM chat_messages WHERE chat_id = c.id AND source IS NOT NULL ORDER BY created_at DESC LIMIT 1) as source
    , (SELECT created_at FROM chat_messages WHERE chat_id = c.id ORDER BY created_at DESC LIMIT 1) as last_message_timestamp
FROM chats c
WHERE c.project_id = @project_id AND c.user_id = @user_id
  AND c.deleted IS FALSE
ORDER BY c.updated_at DESC;

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

-- name: ListLatestGenerationChatMessages :many
-- Returns only the latest-generation rows; older generations are audit-only.
SELECT cm.* FROM chat_messages cm
WHERE cm.chat_id = @chat_id
  AND (cm.project_id IS NULL OR cm.project_id = @project_id::uuid)
  AND cm.generation = (SELECT MAX(generation) FROM chat_messages WHERE chat_id = @chat_id)
ORDER BY cm.seq ASC;

-- name: GetMaxGenerationForChat :one
SELECT COALESCE(MAX(generation), 0)::integer AS generation FROM chat_messages WHERE chat_id = @chat_id;

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
  );

-- name: ListChatsWithResolutions :many
WITH limited_chats AS (
  SELECT
    c.id,
    c.title,
    c.user_id,
    c.external_user_id,
    c.created_at,
    c.updated_at,
    (SELECT COUNT(*) FROM chat_messages WHERE chat_id = c.id)::integer as num_messages,
    (SELECT source FROM chat_messages WHERE chat_id = c.id AND source IS NOT NULL ORDER BY created_at DESC LIMIT 1) as source,
    (SELECT created_at FROM chat_messages WHERE chat_id = c.id ORDER BY created_at DESC LIMIT 1) as last_message_timestamp,
    COALESCE(
      (SELECT AVG(score)::integer FROM chat_resolutions WHERE chat_id = c.id),
      0
    ) as avg_score
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
  ORDER BY
    CASE WHEN @sort_by = 'created_at' AND @sort_order = 'desc' THEN c.created_at END DESC NULLS LAST,
    CASE WHEN @sort_by = 'created_at' AND @sort_order = 'asc' THEN c.created_at END ASC NULLS LAST,
    CASE WHEN @sort_by = 'num_messages' AND @sort_order = 'desc' THEN (SELECT COUNT(*) FROM chat_messages WHERE chat_id = c.id) END DESC NULLS LAST,
    CASE WHEN @sort_by = 'num_messages' AND @sort_order = 'asc' THEN (SELECT COUNT(*) FROM chat_messages WHERE chat_id = c.id) END ASC NULLS LAST,
    CASE WHEN @sort_by = 'score' AND @sort_order = 'desc' THEN COALESCE((SELECT AVG(score) FROM chat_resolutions WHERE chat_id = c.id), 0) END DESC NULLS LAST,
    CASE WHEN @sort_by = 'score' AND @sort_order = 'asc' THEN COALESCE((SELECT AVG(score) FROM chat_resolutions WHERE chat_id = c.id), 0) END ASC NULLS LAST,
    c.created_at DESC
  LIMIT @page_limit
  OFFSET @page_offset
)
SELECT
    lc.id as chat_id,
    lc.title,
    lc.user_id,
    lc.external_user_id,
    lc.source,
    lc.created_at,
    lc.updated_at,
    lc.num_messages,
    lc.last_message_timestamp,
    lc.avg_score,
    cr.id as resolution_id,
    cr.user_goal,
    cr.resolution,
    cr.resolution_notes,
    cr.score,
    cr.created_at as resolution_created_at,
    COALESCE(
        (
            SELECT array_agg(crm.message_id)
            FROM chat_resolution_messages crm
            WHERE crm.chat_resolution_id = cr.id
        ),
        ARRAY[]::uuid[]
    ) as message_ids
FROM limited_chats lc
LEFT JOIN chat_resolutions cr ON cr.chat_id = lc.id
ORDER BY
    CASE WHEN @sort_by = 'created_at' AND @sort_order = 'desc' THEN lc.created_at END DESC NULLS LAST,
    CASE WHEN @sort_by = 'created_at' AND @sort_order = 'asc' THEN lc.created_at END ASC NULLS LAST,
    CASE WHEN @sort_by = 'num_messages' AND @sort_order = 'desc' THEN lc.num_messages END DESC NULLS LAST,
    CASE WHEN @sort_by = 'num_messages' AND @sort_order = 'asc' THEN lc.num_messages END ASC NULLS LAST,
    CASE WHEN @sort_by = 'score' AND @sort_order = 'desc' THEN lc.avg_score END DESC NULLS LAST,
    CASE WHEN @sort_by = 'score' AND @sort_order = 'asc' THEN lc.avg_score END ASC NULLS LAST,
    lc.created_at DESC,
    cr.created_at DESC;

-- name: GetChatWithResolutions :one
SELECT
    c.*,
    (
        COALESCE(
            (SELECT COUNT(*) FROM chat_messages WHERE chat_id = c.id),
            0
        )
    )::integer as num_messages,
    COALESCE(
        (
            SELECT json_agg(
                json_build_object(
                    'id', cr.id,
                    'user_goal', cr.user_goal,
                    'resolution', cr.resolution,
                    'resolution_notes', cr.resolution_notes,
                    'score', cr.score,
                    'created_at', cr.created_at,
                    'message_ids', (
                        SELECT COALESCE(array_agg(crm.message_id), ARRAY[]::uuid[])
                        FROM chat_resolution_messages crm
                        WHERE crm.chat_resolution_id = cr.id
                    )
                ) ORDER BY cr.created_at DESC
            )
            FROM chat_resolutions cr
            WHERE cr.chat_id = c.id
        ),
        '[]'::json
    ) as resolutions
FROM chats c
WHERE c.id = @id AND c.deleted IS FALSE;

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

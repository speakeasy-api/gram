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
FROM chats c
WHERE c.project_id = @project_id
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
FROM chats c
WHERE c.project_id = @project_id AND c.external_user_id = @external_user_id
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
FROM chats c
WHERE c.project_id = @project_id AND c.user_id = @user_id
ORDER BY c.updated_at DESC;

-- name: GetChat :one
SELECT * FROM chats WHERE id = @id;

-- name: ListChatMessages :many
SELECT * FROM chat_messages 
WHERE chat_id = @chat_id AND (project_id IS NULL OR project_id = @project_id::uuid) 
ORDER BY seq ASC;

-- name: CountChatMessages :one
SELECT COUNT(*) FROM chat_messages WHERE chat_id = @chat_id;

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

-- name: ListChatsWithResolutions :many
WITH limited_chats AS (
  SELECT c.id, c.title, c.user_id, c.external_user_id, c.created_at, c.updated_at
  FROM chats c
  WHERE c.project_id = @project_id
    AND c.deleted IS FALSE
    AND (@external_user_id = '' OR c.external_user_id = @external_user_id)
    AND (@from_time::timestamptz IS NULL OR c.created_at >= @from_time)
    AND (@to_time::timestamptz IS NULL OR c.created_at <= @to_time)
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
  ORDER BY c.updated_at DESC
  LIMIT @page_limit
  OFFSET @page_offset
)
SELECT
    lc.id as chat_id,
    lc.title,
    lc.user_id,
    lc.external_user_id,
    lc.created_at,
    lc.updated_at,
    (
        COALESCE(
            (SELECT COUNT(*) FROM chat_messages WHERE chat_id = lc.id),
            0
        )
    )::integer as num_messages,
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
ORDER BY lc.updated_at DESC, cr.created_at DESC;

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
WHERE c.id = @id;

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

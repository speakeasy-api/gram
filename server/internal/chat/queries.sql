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
WHERE c.project_id = @project_id;

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
WHERE c.project_id = @project_id AND c.external_user_id = @external_user_id;


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
WHERE c.project_id = @project_id AND c.user_id = @user_id;

-- name: GetChat :one
SELECT * FROM chats WHERE id = @id;

-- name: ListChatMessages :many
SELECT * FROM chat_messages WHERE chat_id = @chat_id AND (project_id IS NULL OR project_id = @project_id::uuid);

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

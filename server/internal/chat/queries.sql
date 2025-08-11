-- name: UpsertChat :one
INSERT INTO chats (
    id
  , project_id
  , organization_id
  , user_id
  , title
  , created_at
  , updated_at
)
VALUES (
    @id,
    @project_id,
    @organization_id,
    @user_id,
    @title,
    NOW(),
    NOW()
)
ON CONFLICT (id) DO UPDATE SET 
    title = @title,
    updated_at = NOW()
RETURNING id;

-- name: CreateChatMessage :copyfrom
INSERT INTO chat_messages (
    chat_id
  , role
  , project_id
  , content
  , model
  , message_id
  , tool_call_id
  , user_id
  , finish_reason
  , tool_calls
  , prompt_tokens
  , completion_tokens
  , total_tokens
)
VALUES (@chat_id, @role, @project_id::uuid, @content, @model, @message_id, @tool_call_id, @user_id, @finish_reason, @tool_calls, @prompt_tokens, @completion_tokens, @total_tokens);

-- name: ListChats :many
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

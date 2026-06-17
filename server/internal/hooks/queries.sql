-- name: ListHooksServerNameOverrides :many
SELECT id, raw_server_name, display_name, created_at, updated_at
FROM hooks_server_name_overrides
WHERE project_id = $1
ORDER BY display_name, raw_server_name;

-- name: UpsertHooksServerNameOverride :one
INSERT INTO hooks_server_name_overrides (project_id, raw_server_name, display_name)
VALUES ($1, $2, $3)
ON CONFLICT (project_id, raw_server_name)
DO UPDATE SET display_name = EXCLUDED.display_name, updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteHooksServerNameOverride :exec
DELETE FROM hooks_server_name_overrides
WHERE id = $1 AND project_id = $2;

-- name: UpsertClaudeCodeSession :one
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
ON CONFLICT (id) DO UPDATE SET updated_at = NOW()
RETURNING id;

-- name: UpdateClaudeCodeSessionTimestamp :exec
UPDATE chats SET updated_at = NOW() WHERE id = @id AND project_id = @project_id;

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

-- name: InsertShadowMCPBlockResult :exec
INSERT INTO risk_results (
    id
  , project_id
  , organization_id
  , risk_policy_id
  , risk_policy_version
  , chat_message_id
  , source
  , found
  , rule_id
  , description
  , match
  , confidence
)
VALUES (
    sqlc.arg(id)
  , sqlc.arg(project_id)
  , sqlc.arg(organization_id)
  , sqlc.arg(risk_policy_id)
  , sqlc.arg(risk_policy_version)
  , sqlc.arg(chat_message_id)
  , 'shadow_mcp'
  , TRUE
  , 'shadow_mcp.unverified_call'
  , sqlc.arg(description)
  , sqlc.arg(match)
  , sqlc.arg(confidence)
);

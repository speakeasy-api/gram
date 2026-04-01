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
ON CONFLICT (id) DO UPDATE SET updated_at = NOW()
RETURNING id;

-- name: UpdateClaudeCodeSessionTimestamp :exec
UPDATE chats SET updated_at = NOW() WHERE id = @id AND project_id = @project_id;

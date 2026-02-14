-- name: CreateHostedChat :one
INSERT INTO hosted_chats (
    organization_id,
    project_id,
    created_by_user_id,
    name,
    slug,
    mcp_slug,
    system_prompt,
    welcome_title,
    welcome_subtitle,
    theme_color_scheme
) VALUES (
    @organization_id,
    @project_id,
    @created_by_user_id,
    @name,
    @slug,
    @mcp_slug,
    @system_prompt,
    @welcome_title,
    @welcome_subtitle,
    @theme_color_scheme
) RETURNING *;

-- name: GetHostedChatBySlug :one
SELECT * FROM hosted_chats
WHERE project_id = @project_id
  AND slug = @slug
  AND deleted IS FALSE;

-- name: GetHostedChatByID :one
SELECT * FROM hosted_chats
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE;

-- name: ListHostedChatsByProject :many
SELECT * FROM hosted_chats
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateHostedChat :one
UPDATE hosted_chats SET
    name = COALESCE(sqlc.narg('name'), name),
    mcp_slug = COALESCE(sqlc.narg('mcp_slug'), mcp_slug),
    system_prompt = COALESCE(sqlc.narg('system_prompt'), system_prompt),
    welcome_title = COALESCE(sqlc.narg('welcome_title'), welcome_title),
    welcome_subtitle = COALESCE(sqlc.narg('welcome_subtitle'), welcome_subtitle),
    theme_color_scheme = COALESCE(sqlc.narg('theme_color_scheme'), theme_color_scheme),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteHostedChat :exec
UPDATE hosted_chats SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetHostedChatPublicBySlug :one
SELECT
    hc.*,
    p.slug AS project_slug
FROM hosted_chats hc
JOIN projects p ON p.id = hc.project_id AND p.deleted IS FALSE
WHERE hc.slug = @slug
  AND hc.deleted IS FALSE;

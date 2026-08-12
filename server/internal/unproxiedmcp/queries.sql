-- Unproxied MCP Servers

-- name: CreateServer :one
INSERT INTO unproxied_mcp_servers (id, project_id, name, slug, url, description)
VALUES (@id, @project_id, @name, @slug, @url, @description)
RETURNING *;

-- name: GetServerByID :one
SELECT *
FROM unproxied_mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetServerBySlug :one
SELECT *
FROM unproxied_mcp_servers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListServersByProjectID :many
SELECT *
FROM unproxied_mcp_servers
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: DeleteServer :one
UPDATE unproxied_mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

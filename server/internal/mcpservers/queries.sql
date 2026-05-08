-- name: CreateMCPServer :one
INSERT INTO mcp_servers (
    id,
    project_id,
    name,
    slug,
    environment_id,
    remote_mcp_server_id,
    toolset_id,
    visibility
)
VALUES (
    @id,
    @project_id,
    @name,
    @slug,
    @environment_id,
    @remote_mcp_server_id,
    @toolset_id,
    @visibility
)
RETURNING *;

-- name: GetMCPServerByID :one
SELECT *
FROM mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListMCPServersByProjectID :many
SELECT *
FROM mcp_servers
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('remote_mcp_server_id')::uuid IS NULL OR remote_mcp_server_id = sqlc.narg('remote_mcp_server_id')::uuid)
  AND (sqlc.narg('toolset_id')::uuid IS NULL OR toolset_id = sqlc.narg('toolset_id')::uuid)
ORDER BY created_at DESC;

-- name: UpdateMCPServer :one
UPDATE mcp_servers
SET
    name = @name,
    slug = @slug,
    environment_id = @environment_id,
    remote_mcp_server_id = @remote_mcp_server_id,
    toolset_id = @toolset_id,
    visibility = @visibility,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteMCPServer :one
UPDATE mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

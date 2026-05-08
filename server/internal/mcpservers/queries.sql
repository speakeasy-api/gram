-- name: CreateMCPServer :one
INSERT INTO mcp_servers (
    project_id,
    environment_id,
    external_oauth_server_id,
    oauth_proxy_server_id,
    remote_mcp_server_id,
    toolset_id,
    visibility
)
VALUES (
    @project_id,
    @environment_id,
    @external_oauth_server_id,
    @oauth_proxy_server_id,
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
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateMCPServer :one
UPDATE mcp_servers
SET
    environment_id = @environment_id,
    external_oauth_server_id = @external_oauth_server_id,
    oauth_proxy_server_id = @oauth_proxy_server_id,
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

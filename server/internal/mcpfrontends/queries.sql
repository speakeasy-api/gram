-- name: CreateMCPFrontend :one
INSERT INTO mcp_frontends (
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

-- name: GetMCPFrontendByID :one
SELECT *
FROM mcp_frontends
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListMCPFrontendsByProjectID :many
SELECT *
FROM mcp_frontends
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateMCPFrontend :one
UPDATE mcp_frontends
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

-- name: DeleteMCPFrontend :one
UPDATE mcp_frontends
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

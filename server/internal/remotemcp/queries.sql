-- Remote MCP Servers

-- name: CreateServer :one
INSERT INTO remote_mcp_servers (project_id, transport_type, url)
VALUES (@project_id, @transport_type, @url)
RETURNING *;

-- name: GetServerByID :one
SELECT *
FROM remote_mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListServersByProjectID :many
SELECT *
FROM remote_mcp_servers
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateServer :one
UPDATE remote_mcp_servers
SET
    transport_type = COALESCE(@transport_type, transport_type),
    url = COALESCE(@url, url),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteServer :one
UPDATE remote_mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- Remote MCP Server Headers

-- name: ListHeadersByServerID :many
SELECT *
FROM remote_mcp_server_headers
WHERE remote_mcp_server_id = @remote_mcp_server_id AND deleted IS FALSE
ORDER BY name;

-- name: ListHeadersByServerIDs :many
SELECT *
FROM remote_mcp_server_headers
WHERE remote_mcp_server_id = ANY(@remote_mcp_server_ids::uuid[]) AND deleted IS FALSE
ORDER BY remote_mcp_server_id, name;

-- name: CreateHeader :one
INSERT INTO remote_mcp_server_headers (
    remote_mcp_server_id,
    name,
    description,
    is_required,
    is_secret,
    value,
    value_from_request_header
)
VALUES (
    @remote_mcp_server_id,
    @name,
    @description,
    @is_required,
    @is_secret,
    @value,
    @value_from_request_header
)
RETURNING *;

-- name: UpsertHeader :one
INSERT INTO remote_mcp_server_headers (
    remote_mcp_server_id,
    name,
    description,
    is_required,
    is_secret,
    value,
    value_from_request_header
)
VALUES (
    @remote_mcp_server_id,
    @name,
    @description,
    @is_required,
    @is_secret,
    @value,
    @value_from_request_header
)
ON CONFLICT (remote_mcp_server_id, name) WHERE deleted IS FALSE
DO UPDATE SET
    description = EXCLUDED.description,
    is_required = EXCLUDED.is_required,
    is_secret = EXCLUDED.is_secret,
    value = EXCLUDED.value,
    value_from_request_header = EXCLUDED.value_from_request_header,
    updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteHeader :exec
UPDATE remote_mcp_server_headers
SET deleted_at = clock_timestamp()
WHERE remote_mcp_server_id = @remote_mcp_server_id AND name = @name AND deleted IS FALSE;

-- name: DeleteHeadersByServerID :exec
UPDATE remote_mcp_server_headers
SET deleted_at = clock_timestamp()
WHERE remote_mcp_server_id = @remote_mcp_server_id AND deleted IS FALSE;

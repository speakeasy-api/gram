-- name: CreateRemoteMCPServer :one
INSERT INTO remote_mcp_servers (
    project_id
  , transport_type
  , url
) VALUES (
    @project_id
  , @transport_type
  , @url
)
RETURNING *;

-- name: GetRemoteMCPServerByID :one
SELECT *
FROM remote_mcp_servers
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListRemoteMCPServersByProject :many
SELECT *
FROM remote_mcp_servers
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateRemoteMCPServer :one
UPDATE remote_mcp_servers
SET
    transport_type = COALESCE(sqlc.narg('transport_type'), transport_type)
  , url = COALESCE(sqlc.narg('url'), url)
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteRemoteMCPServer :exec
UPDATE remote_mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: CreateRemoteMCPServerHeader :one
-- Tenant isolation: insert is guarded by a subquery that requires the parent
-- remote_mcp_servers row to belong to the supplied project_id.
INSERT INTO remote_mcp_server_headers (
    remote_mcp_server_id
  , name
  , description
  , is_required
  , is_secret
  , value
  , value_from_request_header
)
SELECT
    s.id
  , @name
  , @description
  , @is_required
  , @is_secret
  , sqlc.narg('value')
  , sqlc.narg('value_from_request_header')
FROM remote_mcp_servers s
WHERE s.id = @remote_mcp_server_id
  AND s.project_id = @project_id
  AND s.deleted IS FALSE
RETURNING *;

-- name: GetRemoteMCPServerHeaderByID :one
SELECT h.*
FROM remote_mcp_server_headers h
JOIN remote_mcp_servers s ON h.remote_mcp_server_id = s.id
WHERE h.id = @id
  AND s.project_id = @project_id
  AND h.deleted IS FALSE
  AND s.deleted IS FALSE;

-- name: ListRemoteMCPServerHeadersByServer :many
SELECT h.*
FROM remote_mcp_server_headers h
JOIN remote_mcp_servers s ON h.remote_mcp_server_id = s.id
WHERE s.id = @remote_mcp_server_id
  AND s.project_id = @project_id
  AND h.deleted IS FALSE
  AND s.deleted IS FALSE
ORDER BY h.name ASC;

-- name: UpdateRemoteMCPServerHeader :one
-- The value / value_from_request_header pair is XOR-constrained at the schema
-- level, so this update sets both atomically instead of using COALESCE on them
-- individually. Callers supply both on every call to switch modes safely.
UPDATE remote_mcp_server_headers h
SET
    description = COALESCE(sqlc.narg('description'), h.description)
  , is_required = COALESCE(sqlc.narg('is_required'), h.is_required)
  , is_secret = COALESCE(sqlc.narg('is_secret'), h.is_secret)
  , value = sqlc.narg('value')
  , value_from_request_header = sqlc.narg('value_from_request_header')
  , updated_at = clock_timestamp()
FROM remote_mcp_servers s
WHERE h.id = @id
  AND h.remote_mcp_server_id = s.id
  AND s.project_id = @project_id
  AND h.deleted IS FALSE
  AND s.deleted IS FALSE
RETURNING h.*;

-- name: DeleteRemoteMCPServerHeader :exec
UPDATE remote_mcp_server_headers h
SET deleted_at = clock_timestamp()
FROM remote_mcp_servers s
WHERE h.id = @id
  AND h.remote_mcp_server_id = s.id
  AND s.project_id = @project_id
  AND h.deleted IS FALSE
  AND s.deleted IS FALSE;

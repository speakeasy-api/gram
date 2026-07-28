-- Remote MCP Servers

-- name: CreateServer :one
INSERT INTO remote_mcp_servers (id, project_id, name, slug, transport_type, url)
VALUES (@id, @project_id, @name, @slug, @transport_type, @url)
RETURNING *;

-- name: GetServerByID :one
SELECT *
FROM remote_mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetServerBySlug :one
SELECT *
FROM remote_mcp_servers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListServersByProjectID :many
SELECT *
FROM remote_mcp_servers
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateServer :one
UPDATE remote_mcp_servers
SET
    name = @name,
    slug = @slug,
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

-- The remote_mcp_server_headers table has no project_id column of its own, so
-- every management query below pins the project by subselecting the parent
-- remote_mcp_servers row. Without that, a caller could address another
-- project's header by guessing its id.

-- name: ListHeadersByServerID :many
-- Not project-scoped. Serves the MCP proxy (internal/mcp/serveendpoint.go),
-- which has already resolved the server row and needs decrypted header values
-- to inject into outbound requests. Management reads use ListServerHeaders.
SELECT *
FROM remote_mcp_server_headers
WHERE remote_mcp_server_id = @remote_mcp_server_id AND deleted IS FALSE
ORDER BY name;

-- name: ListServerHeaders :many
SELECT *
FROM remote_mcp_server_headers
WHERE remote_mcp_server_id = @remote_mcp_server_id
    AND deleted IS FALSE
    AND remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    )
ORDER BY name;

-- name: GetServerHeader :one
SELECT *
FROM remote_mcp_server_headers
WHERE remote_mcp_server_headers.id = @id
    AND remote_mcp_server_headers.deleted IS FALSE
    AND remote_mcp_server_headers.remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    );

-- name: CreateServerHeader :one
-- Plain INSERT (never an upsert) so a live name collision raises a unique
-- violation the caller maps to 409 rather than silently overwriting the
-- existing header. The INSERT ... SELECT yields zero rows when the parent
-- server is missing or belongs to another project, which the caller maps to 404.
INSERT INTO remote_mcp_server_headers (
    remote_mcp_server_id,
    name,
    description,
    is_required,
    is_secret,
    value,
    value_from_request_header
)
SELECT
    remote_mcp_servers.id,
    @name::text,
    sqlc.narg(description)::text,
    @is_required::boolean,
    @is_secret::boolean,
    sqlc.narg(value)::text,
    sqlc.narg(value_from_request_header)::text
FROM remote_mcp_servers
WHERE remote_mcp_servers.id = @remote_mcp_server_id
    AND remote_mcp_servers.project_id = @project_id
    AND remote_mcp_servers.deleted IS FALSE
RETURNING *;

-- name: UpdateServerHeader :one
-- Full replace of the mutable fields, with one exception: when set_value is
-- false the caller omitted a value for an existing secret header, so the stored
-- ciphertext is left in place. Preserving it in SQL (rather than reading it out
-- and writing it back) keeps the encrypted value inside the database and makes
-- double-encryption impossible. It also keeps the row satisfying
-- remote_mcp_server_headers_value_source_check, which a plain
-- "value = NULL" write would violate for a secret header.
UPDATE remote_mcp_server_headers
SET
    name = @name::text,
    description = sqlc.narg(description)::text,
    is_required = @is_required::boolean,
    is_secret = @is_secret::boolean,
    value = CASE WHEN @set_value::boolean THEN sqlc.narg(value)::text ELSE value END,
    value_from_request_header = sqlc.narg(value_from_request_header)::text,
    updated_at = clock_timestamp()
WHERE remote_mcp_server_headers.id = @id
    AND remote_mcp_server_headers.deleted IS FALSE
    AND remote_mcp_server_headers.remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    )
RETURNING *;

-- name: DeleteServerHeader :one
-- Returns the soft-deleted row so the caller can emit an audit event carrying
-- the header's name.
UPDATE remote_mcp_server_headers
SET deleted_at = clock_timestamp()
WHERE remote_mcp_server_headers.id = @id
    AND remote_mcp_server_headers.deleted IS FALSE
    AND remote_mcp_server_headers.remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    )
RETURNING *;

-- name: DeleteHeadersByServerID :exec
-- Soft-delete every header of a server. The FK's ON DELETE CASCADE does not
-- fire for soft deletes, so deleteServer calls this explicitly. The affected
-- rows are not returned: the cascade is covered by the parent's
-- remote-mcp:delete audit entry and emits no per-header events. Runs before the
-- parent row is tombstoned, so the parent is still visible to the project
-- subselect.
UPDATE remote_mcp_server_headers
SET deleted_at = clock_timestamp()
WHERE remote_mcp_server_id = @remote_mcp_server_id
    AND deleted IS FALSE
    AND remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    );

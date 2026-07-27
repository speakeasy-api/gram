-- Tunneled MCP Servers

-- name: LockOrganizationTunneledMcpLimit :exec
-- Serialize per-org creates so concurrent requests cannot bypass the source cap.
SELECT pg_advisory_xact_lock(hashtext('tunneled_mcp_limit:' || @organization_id::text));

-- name: GetTunneledMcpServerLimitByOrganizationID :one
SELECT billing_metadata.tunneled_mcp_server_limit AS tunneled_mcp_server_limit
FROM organization_metadata
LEFT JOIN billing_metadata ON billing_metadata.organization_id = organization_metadata.id
WHERE organization_metadata.id = @organization_id;

-- name: CountActiveServersByOrganizationID :one
SELECT COUNT(*)
FROM tunneled_mcp_servers
JOIN projects ON projects.id = tunneled_mcp_servers.project_id
WHERE projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND tunneled_mcp_servers.deleted IS FALSE;

-- name: CreateServer :one
INSERT INTO tunneled_mcp_servers (id, project_id, name, key_hash, key_prefix)
VALUES (@id, @project_id, @name, @key_hash, @key_prefix)
RETURNING *;

-- name: ListServersByProjectID :many
SELECT *
FROM tunneled_mcp_servers
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: GetServerByID :one
SELECT *
FROM tunneled_mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: UpdateServer :one
UPDATE tunneled_mcp_servers
SET
    name = @name,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: RotateServerKey :one
UPDATE tunneled_mcp_servers
SET
    key_hash = @key_hash,
    key_prefix = @key_prefix,
    status = 'created',
    agent_version = NULL,
    last_seen_at = NULL,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteServer :one
UPDATE tunneled_mcp_servers
SET
    status = 'revoked',
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- Tunneled MCP Server Headers
--
-- every management query below pins the project by subselecting the parent
-- tunneled_mcp_servers row. Without that, a caller could address another
-- project's header by guessing its id.

-- name: ListHeadersByServerID :many
-- Not project-scoped. Serves the MCP proxy (internal/mcp/tunnel_manager.go),
-- which has already resolved the server row and needs decrypted header values
-- to inject into outbound requests. Management reads use ListServerHeaders.
SELECT *
FROM tunneled_mcp_server_headers
WHERE tunneled_mcp_server_id = @tunneled_mcp_server_id AND deleted IS FALSE
ORDER BY name;

-- name: ListServerHeaders :many
SELECT *
FROM tunneled_mcp_server_headers
WHERE tunneled_mcp_server_id = @tunneled_mcp_server_id
    AND deleted IS FALSE
    AND tunneled_mcp_server_id IN (
        SELECT tunneled_mcp_servers.id FROM tunneled_mcp_servers
        WHERE tunneled_mcp_servers.project_id = @project_id AND tunneled_mcp_servers.deleted IS FALSE
    )
ORDER BY name;

-- name: GetServerHeader :one
SELECT *
FROM tunneled_mcp_server_headers
WHERE tunneled_mcp_server_headers.id = @id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_server_headers.tunneled_mcp_server_id IN (
        SELECT tunneled_mcp_servers.id FROM tunneled_mcp_servers
        WHERE tunneled_mcp_servers.project_id = @project_id AND tunneled_mcp_servers.deleted IS FALSE
    );

-- name: CreateServerHeader :one
-- Plain INSERT (never an upsert) so a live name collision raises a unique
-- violation the caller maps to 409 rather than silently overwriting the
-- existing header. The INSERT ... SELECT yields zero rows when the parent
-- server is missing or belongs to another project, which the caller maps to 404.
INSERT INTO tunneled_mcp_server_headers (
    tunneled_mcp_server_id,
    name,
    description,
    is_required,
    is_secret,
    value,
    value_from_request_header
)
SELECT
    tunneled_mcp_servers.id,
    @name::text,
    sqlc.narg(description)::text,
    @is_required::boolean,
    @is_secret::boolean,
    sqlc.narg(value)::text,
    sqlc.narg(value_from_request_header)::text
FROM tunneled_mcp_servers
WHERE tunneled_mcp_servers.id = @tunneled_mcp_server_id
    AND tunneled_mcp_servers.project_id = @project_id
    AND tunneled_mcp_servers.deleted IS FALSE
RETURNING *;

-- name: UpdateServerHeader :one
-- Full replace of the mutable fields, with one exception: when set_value is
-- false the caller omitted a value for an existing secret header, so the stored
-- ciphertext is left in place. Preserving it in SQL (rather than reading it out
-- and writing it back) keeps the encrypted value inside the database and makes
-- double-encryption impossible. It also keeps the row satisfying
-- tunneled_mcp_server_headers_value_source_check, which a plain
-- "value = NULL" write would violate for a secret header.
UPDATE tunneled_mcp_server_headers
SET
    name = @name::text,
    description = sqlc.narg(description)::text,
    is_required = @is_required::boolean,
    is_secret = @is_secret::boolean,
    value = CASE WHEN @set_value::boolean THEN sqlc.narg(value)::text ELSE value END,
    value_from_request_header = sqlc.narg(value_from_request_header)::text,
    updated_at = clock_timestamp()
WHERE tunneled_mcp_server_headers.id = @id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_server_headers.tunneled_mcp_server_id IN (
        SELECT tunneled_mcp_servers.id FROM tunneled_mcp_servers
        WHERE tunneled_mcp_servers.project_id = @project_id AND tunneled_mcp_servers.deleted IS FALSE
    )
RETURNING *;

-- name: DeleteServerHeader :one
-- Returns the soft-deleted row so the caller can emit an audit event carrying
-- the header's name.
UPDATE tunneled_mcp_server_headers
SET deleted_at = clock_timestamp()
WHERE tunneled_mcp_server_headers.id = @id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_server_headers.tunneled_mcp_server_id IN (
        SELECT tunneled_mcp_servers.id FROM tunneled_mcp_servers
        WHERE tunneled_mcp_servers.project_id = @project_id AND tunneled_mcp_servers.deleted IS FALSE
    )
RETURNING *;

-- name: DeleteHeadersByServerID :exec
-- Soft-delete every header of a server. The FK's ON DELETE CASCADE does not
-- fire for soft deletes, so deleteServer calls this explicitly. The affected
-- rows are not returned: the cascade is covered by the parent's
-- tunneled-mcp:delete audit entry and emits no per-header events. Runs before
-- the parent row is tombstoned, so the parent is still visible to the project
-- subselect.
UPDATE tunneled_mcp_server_headers
SET deleted_at = clock_timestamp()
WHERE tunneled_mcp_server_id = @tunneled_mcp_server_id
    AND deleted IS FALSE
    AND tunneled_mcp_server_id IN (
        SELECT tunneled_mcp_servers.id FROM tunneled_mcp_servers
        WHERE tunneled_mcp_servers.project_id = @project_id AND tunneled_mcp_servers.deleted IS FALSE
    );

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
INSERT INTO tunneled_mcp_servers (id, project_id, name, key_hash, key_prefix, resource_identifier)
VALUES (@id, @project_id, @name, @key_hash, @key_prefix, @resource_identifier)
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

-- name: GetServerByIDForUpdate :one
-- Row-locking read used inside the update transaction so a concurrent
-- enable/disable of allow_public cannot compute a stale before->after
-- transition (which drives the anonymous-session revocation).
SELECT *
FROM tunneled_mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
FOR UPDATE;

-- name: UpdateServer :one
UPDATE tunneled_mcp_servers
SET
    name = COALESCE(sqlc.narg('name'), name),
    allow_public = COALESCE(sqlc.narg('allow_public'), allow_public),
    -- Tri-state: NULL leaves the stored value, empty string clears to NULL.
    resource_identifier = CASE
        WHEN sqlc.narg('resource_identifier')::text IS NULL THEN resource_identifier
        ELSE NULLIF(sqlc.narg('resource_identifier')::text, '')
    END,
    -- Public admission limit, tri-state per column: NULL leaves the stored
    -- value, 0 clears to NULL (deployment default), any other value is stored.
    -- Bounds are enforced by the column CHECK constraints.
    public_request_rate_per_second = CASE
        WHEN sqlc.narg('public_request_rate_per_second')::int IS NULL THEN public_request_rate_per_second
        ELSE NULLIF(sqlc.narg('public_request_rate_per_second')::int, 0)
    END,
    public_request_burst = CASE
        WHEN sqlc.narg('public_request_burst')::int IS NULL THEN public_request_burst
        ELSE NULLIF(sqlc.narg('public_request_burst')::int, 0)
    END,
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
-- tunneled_mcp_server_headers has no project_id column. Every management
-- query pins the project through the parent tunneled_mcp_servers row so a
-- caller cannot address another project's header by guessing its id.

-- name: ListHeadersByServerID :many
-- Serves the MCP proxy, which needs the stored values to inject into outbound
-- requests. Scoped to the project of the MCP server being served, so a tunnel
-- id from anywhere else yields nothing. Management reads use ListServerHeaders.
SELECT tunneled_mcp_server_headers.*
FROM tunneled_mcp_server_headers
JOIN tunneled_mcp_servers ON tunneled_mcp_servers.id = tunneled_mcp_server_headers.tunneled_mcp_server_id
WHERE tunneled_mcp_server_headers.tunneled_mcp_server_id = @tunneled_mcp_server_id
    AND tunneled_mcp_servers.project_id = @project_id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_servers.deleted IS FALSE
ORDER BY tunneled_mcp_server_headers.name;

-- name: ListServerHeaders :many
SELECT tunneled_mcp_server_headers.*
FROM tunneled_mcp_server_headers
JOIN tunneled_mcp_servers ON tunneled_mcp_servers.id = tunneled_mcp_server_headers.tunneled_mcp_server_id
WHERE tunneled_mcp_server_headers.tunneled_mcp_server_id = @tunneled_mcp_server_id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_servers.project_id = @project_id
    AND tunneled_mcp_servers.deleted IS FALSE
ORDER BY tunneled_mcp_server_headers.name;

-- name: GetServerHeader :one
SELECT tunneled_mcp_server_headers.*
FROM tunneled_mcp_server_headers
JOIN tunneled_mcp_servers ON tunneled_mcp_servers.id = tunneled_mcp_server_headers.tunneled_mcp_server_id
WHERE tunneled_mcp_server_headers.id = @id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_servers.project_id = @project_id
    AND tunneled_mcp_servers.deleted IS FALSE;

-- name: FindLiveServerHeaderByName :one
-- Looks up a live header on a tunnel whose name collides with the given one,
-- other than the header being updated. Names collide case-insensitively and
-- with underscores read as dashes, since some upstream servers fold X_Foo into
-- X-Foo. Runs with the parent row locked, so a concurrent writer to the same
-- tunnel cannot slip in between the check and the write.
SELECT tunneled_mcp_server_headers.id
FROM tunneled_mcp_server_headers
JOIN tunneled_mcp_servers ON tunneled_mcp_servers.id = tunneled_mcp_server_headers.tunneled_mcp_server_id
WHERE tunneled_mcp_server_headers.tunneled_mcp_server_id = @tunneled_mcp_server_id
    AND tunneled_mcp_servers.project_id = @project_id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND replace(lower(tunneled_mcp_server_headers.name), '_', '-') = replace(lower(@name::text), '_', '-')
    AND tunneled_mcp_server_headers.id <> @exclude_id::uuid
LIMIT 1;

-- name: CreateServerHeader :one
-- Plain INSERT (never an upsert) so a live name collision raises a unique
-- violation rather than overwriting the existing header. The INSERT ... SELECT
-- yields zero rows when the parent is missing or belongs to another project.
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
-- Full replace of the mutable fields, except that when set_value is false the
-- stored value is left in place. That is how omitting the value of an existing
-- secret preserves it without its ciphertext leaving the database.
UPDATE tunneled_mcp_server_headers
SET
    name = @name::text,
    description = sqlc.narg(description)::text,
    is_required = @is_required::boolean,
    is_secret = @is_secret::boolean,
    value = CASE WHEN @set_value::boolean THEN sqlc.narg(value)::text ELSE value END,
    value_from_request_header = sqlc.narg(value_from_request_header)::text,
    updated_at = clock_timestamp()
FROM tunneled_mcp_servers
WHERE tunneled_mcp_server_headers.id = @id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_servers.id = tunneled_mcp_server_headers.tunneled_mcp_server_id
    AND tunneled_mcp_servers.project_id = @project_id
    AND tunneled_mcp_servers.deleted IS FALSE
RETURNING tunneled_mcp_server_headers.*;

-- name: DeleteServerHeader :one
-- Returns the soft-deleted row so the caller can audit it.
UPDATE tunneled_mcp_server_headers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM tunneled_mcp_servers
WHERE tunneled_mcp_server_headers.id = @id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_servers.id = tunneled_mcp_server_headers.tunneled_mcp_server_id
    AND tunneled_mcp_servers.project_id = @project_id
    AND tunneled_mcp_servers.deleted IS FALSE
RETURNING tunneled_mcp_server_headers.*;

-- name: DeleteHeadersByServerID :many
-- Soft-deletes every live header of a tunnel. ON DELETE CASCADE only fires on
-- hard deletes, so deleteServer calls this explicitly, with the parent row
-- locked and before tombstoning it, and audits each returned row.
UPDATE tunneled_mcp_server_headers
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
FROM tunneled_mcp_servers
WHERE tunneled_mcp_server_headers.tunneled_mcp_server_id = @tunneled_mcp_server_id
    AND tunneled_mcp_server_headers.deleted IS FALSE
    AND tunneled_mcp_servers.id = tunneled_mcp_server_headers.tunneled_mcp_server_id
    AND tunneled_mcp_servers.project_id = @project_id
RETURNING tunneled_mcp_server_headers.*;

-- name: CountLiveServerHeaders :one
-- Counts a tunnel's live headers regardless of the tunnel's own state, so a
-- caller can detect a header that outlived a deleted tunnel.
SELECT COUNT(*)
FROM tunneled_mcp_server_headers
WHERE tunneled_mcp_server_id = @tunneled_mcp_server_id AND deleted IS FALSE;

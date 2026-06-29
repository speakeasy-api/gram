-- Tunnelled MCP Servers

-- name: LockOrganizationTunnelledMcpLimit :exec
-- Serialize create checks per organization so concurrent creates cannot race
-- past the org-level source cap.
SELECT pg_advisory_xact_lock(hashtext('tunnelled_mcp_limit:' || @organization_id::text));

-- name: GetTunnelledMcpServerLimitByOrganizationID :one
SELECT billing_metadata.tunnelled_mcp_server_limit
FROM organization_metadata
LEFT JOIN billing_metadata ON billing_metadata.organization_id = organization_metadata.id
WHERE organization_metadata.id = @organization_id;

-- name: CountActiveServersByOrganizationID :one
SELECT COUNT(*)
FROM tunnelled_mcp_servers
JOIN projects ON projects.id = tunnelled_mcp_servers.project_id
WHERE projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND tunnelled_mcp_servers.deleted IS FALSE;

-- name: CreateServer :one
INSERT INTO tunnelled_mcp_servers (id, project_id, name, key_hash, key_prefix)
VALUES (@id, @project_id, @name, @key_hash, @key_prefix)
RETURNING *;

-- name: ListServersByProjectID :many
SELECT *
FROM tunnelled_mcp_servers
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: GetServerByID :one
SELECT *
FROM tunnelled_mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: UpdateServer :one
UPDATE tunnelled_mcp_servers
SET
    name = @name,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: RotateServerKey :one
UPDATE tunnelled_mcp_servers
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
UPDATE tunnelled_mcp_servers
SET
    status = 'revoked',
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: ListAuthClients :many
SELECT ac.*
FROM mcp_auth_clients ac
WHERE ac.organization_id = @organization_id
  AND ac.deleted IS FALSE
  AND (sqlc.narg(client_type)::text IS NULL OR ac.client_type = sqlc.narg(client_type)::text)
  AND (
    sqlc.narg(mcp_server_id)::uuid IS NULL
    OR EXISTS (
      SELECT 1
      FROM mcp_server_auth_clients msac
      WHERE msac.auth_client_id = ac.id
        AND msac.organization_id = ac.organization_id
        AND msac.mcp_server_id = sqlc.narg(mcp_server_id)::uuid
        AND msac.deleted IS FALSE
    )
  )
  AND (sqlc.narg(cursor)::uuid IS NULL OR ac.id > sqlc.narg(cursor)::uuid)
ORDER BY ac.id
LIMIT @limit_value;

-- name: GetAuthClient :one
SELECT ac.*
FROM mcp_auth_clients ac
WHERE ac.id = @id
  AND ac.organization_id = @organization_id
  AND ac.deleted IS FALSE;

-- name: LockAuthClient :one
SELECT ac.*
FROM mcp_auth_clients ac
WHERE ac.id = @id
  AND ac.organization_id = @organization_id
  AND ac.deleted IS FALSE
FOR UPDATE;

-- name: UpdateAuthClient :one
UPDATE mcp_auth_clients
SET name = @name,
    enabled = @enabled,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: ListAuthClientServers :many
SELECT
  msac.auth_client_id,
  ms.id,
  ms.project_id,
  ms.name,
  ms.slug,
  ms.visibility
FROM mcp_server_auth_clients msac
JOIN mcp_servers ms
  ON ms.id = msac.mcp_server_id
 AND ms.project_id = msac.project_id
 AND ms.deleted IS FALSE
JOIN projects p
  ON p.id = ms.project_id
 AND p.organization_id = msac.organization_id
 AND p.deleted IS FALSE
WHERE msac.organization_id = @organization_id
  AND msac.auth_client_id = ANY(@auth_client_ids::uuid[])
  AND msac.deleted IS FALSE
ORDER BY msac.auth_client_id, ms.id;

-- name: LockAuthClientServerRequirements :many
SELECT
  ms.id,
  ms.requires_upstream_auth
FROM mcp_server_auth_clients msac
JOIN mcp_servers ms
  ON ms.id = msac.mcp_server_id
 AND ms.project_id = msac.project_id
 AND ms.deleted IS FALSE
JOIN projects p
  ON p.id = ms.project_id
 AND p.organization_id = msac.organization_id
 AND p.deleted IS FALSE
WHERE msac.organization_id = @organization_id
  AND msac.auth_client_id = @auth_client_id
  AND msac.deleted IS FALSE
ORDER BY ms.id
FOR SHARE OF ms;

-- name: ListAuthClientHeaders :many
SELECT
  h.auth_client_id,
  h.name,
  h.description,
  h.is_required,
  h.is_secret,
  h.value_from_request_header,
  (h.value IS NOT NULL)::boolean AS has_stored_value
FROM mcp_auth_client_headers h
WHERE h.organization_id = @organization_id
  AND h.auth_client_id = ANY(@auth_client_ids::uuid[])
  AND h.deleted IS FALSE
ORDER BY h.auth_client_id, h.name;

-- name: DetachAuthClientFromServer :one
-- Soft-deletes the live link and marks it administrator-owned (source
-- 'admin'). That deleted admin row is a durable detach: the backfill never
-- relinks the pair from the configuration it derived the link from, while its
-- own retirements stay source 'backfill' and are relinked if their evidence
-- returns. The upstream configuration and the client's other links are
-- untouched.
UPDATE mcp_server_auth_clients msac
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp(),
    source = 'admin'
FROM mcp_servers ms
WHERE msac.auth_client_id = @auth_client_id
  AND msac.mcp_server_id = @mcp_server_id
  AND msac.organization_id = @organization_id
  AND msac.deleted IS FALSE
  AND ms.id = msac.mcp_server_id
  AND ms.project_id = msac.project_id
RETURNING msac.id, msac.project_id, ms.name AS mcp_server_name;

-- name: GetOrganizationMCPServer :one
SELECT
  ms.id,
  ms.project_id,
  ms.name,
  ms.slug,
  ms.visibility,
  ms.requires_upstream_auth,
  ms.toolset_id
FROM mcp_servers ms
JOIN projects p
  ON p.id = ms.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
WHERE ms.id = @id
  AND ms.deleted IS FALSE;

-- name: LockOrganizationMCPServer :one
SELECT
  ms.id,
  ms.project_id,
  ms.name,
  ms.slug,
  ms.visibility,
  ms.requires_upstream_auth,
  ms.toolset_id
FROM mcp_servers ms
JOIN projects p
  ON p.id = ms.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
WHERE ms.id = @id
  AND ms.deleted IS FALSE
FOR UPDATE OF ms;

-- name: SetMCPServerRequiresUpstreamAuth :exec
UPDATE mcp_servers
SET requires_upstream_auth = @requires_upstream_auth,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListServerAuthClients :many
SELECT ac.*
FROM mcp_server_auth_clients msac
JOIN mcp_auth_clients ac
  ON ac.id = msac.auth_client_id
 AND ac.organization_id = msac.organization_id
 AND ac.deleted IS FALSE
WHERE msac.mcp_server_id = @mcp_server_id
  AND msac.project_id = @project_id
  AND msac.organization_id = @organization_id
  AND msac.deleted IS FALSE
ORDER BY ac.name, ac.id;

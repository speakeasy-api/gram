-- name: ListAuthClients :many
SELECT ac.*
FROM mcp_auth_clients ac
WHERE ac.organization_id = @organization_id
  AND ac.deleted IS FALSE
  AND (ac.project_id IS NULL OR EXISTS (
    SELECT 1
    FROM projects p
    WHERE p.id = ac.project_id
      AND p.organization_id = ac.organization_id
      AND p.deleted IS FALSE
  ))
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
-- A project client of a deleted project is gone with its project.
SELECT ac.*
FROM mcp_auth_clients ac
WHERE ac.id = @id
  AND ac.organization_id = @organization_id
  AND ac.deleted IS FALSE
  AND (ac.project_id IS NULL OR EXISTS (
    SELECT 1
    FROM projects p
    WHERE p.id = ac.project_id
      AND p.organization_id = ac.organization_id
      AND p.deleted IS FALSE
  ));

-- name: LockAuthClient :one
SELECT ac.*
FROM mcp_auth_clients ac
WHERE ac.id = @id
  AND ac.organization_id = @organization_id
  AND ac.deleted IS FALSE
  AND (ac.project_id IS NULL OR EXISTS (
    SELECT 1
    FROM projects p
    WHERE p.id = ac.project_id
      AND p.organization_id = ac.organization_id
      AND p.deleted IS FALSE
  ))
FOR UPDATE OF ac;

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

-- name: SetMCPServerRequiresUpstreamAuth :execrows
UPDATE mcp_servers ms
SET requires_upstream_auth = @requires_upstream_auth,
    updated_at = clock_timestamp()
FROM projects p
WHERE ms.id = @id
  AND ms.project_id = @project_id
  AND ms.deleted IS FALSE
  AND p.id = ms.project_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE;

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

-- name: GetGitHubAppCredential :one
SELECT g.*
FROM mcp_auth_client_github_apps g
JOIN mcp_auth_clients ac ON ac.id = g.auth_client_id AND ac.organization_id = g.organization_id
WHERE g.auth_client_id = @auth_client_id AND g.organization_id = @organization_id
  AND ac.client_type = 'service_account' AND ac.remote_session_client_id IS NULL
  AND ac.enabled AND NOT ac.deleted
  AND (ac.project_id IS NULL OR EXISTS (SELECT 1 FROM projects p WHERE p.id = ac.project_id AND p.organization_id = ac.organization_id AND NOT p.deleted));

-- name: GetGitHubAppConfiguration :one
SELECT instance_url, app_id, installation_id, upstream_origin
FROM mcp_auth_client_github_apps
WHERE auth_client_id = @auth_client_id AND organization_id = @organization_id;

-- name: CreateGitHubAppAuthClient :one
INSERT INTO mcp_auth_clients (organization_id, project_id, client_type, source, name)
VALUES (@organization_id, @project_id, 'service_account', 'admin', @name)
RETURNING *;

-- name: SetGitHubAppCredential :exec
INSERT INTO mcp_auth_client_github_apps
(auth_client_id, organization_id, instance_url, app_id, installation_id, private_key_encrypted, upstream_origin)
VALUES (@auth_client_id, @organization_id, @instance_url, @app_id, @installation_id, @private_key_encrypted, @upstream_origin)
ON CONFLICT (auth_client_id) DO UPDATE SET
instance_url = EXCLUDED.instance_url, app_id = EXCLUDED.app_id,
installation_id = EXCLUDED.installation_id, private_key_encrypted = EXCLUDED.private_key_encrypted,
upstream_origin = EXCLUDED.upstream_origin, credential_version = generate_uuidv7(), updated_at = clock_timestamp()
WHERE mcp_auth_client_github_apps.organization_id = EXCLUDED.organization_id;

-- name: AttachGitHubAppAuthClient :exec
INSERT INTO mcp_server_auth_clients (organization_id, project_id, mcp_server_id, auth_client_id, client_attachment_scope, source)
SELECT ac.organization_id, @project_id::uuid, @mcp_server_id, ac.id, ac.attachment_scope, 'admin'
FROM mcp_auth_clients ac
WHERE ac.id = @auth_client_id AND ac.organization_id = @organization_id AND NOT ac.deleted
ON CONFLICT (mcp_server_id, auth_client_id) WHERE deleted IS FALSE DO UPDATE SET deleted_at = NULL, source = 'admin', updated_at = clock_timestamp();

-- name: ServerHasGitHubAppAuthClient :one
SELECT EXISTS (
 SELECT 1 FROM mcp_server_auth_clients a
 JOIN mcp_auth_client_github_apps g ON g.auth_client_id = a.auth_client_id AND g.organization_id = a.organization_id
 WHERE a.mcp_server_id = @mcp_server_id AND a.organization_id = @organization_id
) AS present;

-- name: GetGitHubAppServerURL :one
SELECT r.url FROM mcp_servers m
JOIN projects p ON p.id = m.project_id AND p.organization_id = @organization_id AND NOT p.deleted
JOIN remote_mcp_servers r ON r.id = m.remote_mcp_server_id AND r.project_id = m.project_id AND NOT r.deleted
WHERE m.id = @mcp_server_id AND NOT m.deleted;

-- name: ReplaceServerAuthWithGitHubApp :exec
WITH retired AS (
 UPDATE mcp_server_auth_clients SET deleted_at = clock_timestamp(), source = 'admin', updated_at = clock_timestamp()
 WHERE mcp_server_id = @mcp_server_id AND organization_id = @organization_id
   AND auth_client_id <> @auth_client_id AND NOT deleted
), oauth AS (
 UPDATE mcp_server_remote_session_clients SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
 WHERE mcp_server_id = @mcp_server_id AND organization_id = @organization_id AND NOT deleted
)
UPDATE mcp_servers m SET requires_upstream_auth = TRUE, updated_at = clock_timestamp()
FROM projects p WHERE m.id = @mcp_server_id AND m.project_id = p.id
 AND p.organization_id = @organization_id AND NOT m.deleted AND NOT p.deleted;

-- name: ListGitHubAppServers :many
SELECT DISTINCT a.mcp_server_id FROM mcp_server_auth_clients a
JOIN mcp_auth_client_github_apps g ON g.auth_client_id = a.auth_client_id AND g.organization_id = a.organization_id
WHERE a.organization_id = @organization_id AND a.mcp_server_id = ANY(@mcp_server_ids::uuid[]);

-- name: ListGitHubAppConfigurations :many
SELECT auth_client_id, instance_url, app_id, installation_id, upstream_origin
FROM mcp_auth_client_github_apps
WHERE organization_id = @organization_id AND auth_client_id = ANY(@auth_client_ids::uuid[]);

-- name: LockGitHubAppServerURL :one
SELECT r.url FROM mcp_servers m
JOIN projects p ON p.id = m.project_id AND p.organization_id = @organization_id AND NOT p.deleted
JOIN remote_mcp_servers r ON r.id = m.remote_mcp_server_id AND r.project_id = m.project_id AND NOT r.deleted
WHERE m.id = @mcp_server_id AND NOT m.deleted
FOR UPDATE OF m, r;

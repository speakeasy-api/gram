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

-- name: GetAuthClientRegistration :one
-- An OAuth registration a new remote_session or service_account client may
-- name: live, with a live issuer, and in the client's project, shared across
-- the organization, or platform-global.
SELECT c.id,
       c.attachment_scope,
       c.credential_owner,
       c.project_id,
       c.organization_id
FROM remote_session_clients AS c
JOIN remote_session_issuers AS rsi
  ON rsi.id = c.remote_session_issuer_id
 AND rsi.deleted IS FALSE
 AND (rsi.organization_id IS NULL OR rsi.organization_id = @organization_id::text)
WHERE c.id = @id
  AND c.deleted IS FALSE
  AND (
    (c.project_id IS NOT NULL AND c.project_id = sqlc.narg(project_id)::uuid)
    OR (c.project_id IS NULL AND c.organization_id = @organization_id::text)
    OR (c.project_id IS NULL AND c.organization_id IS NULL)
  )
  AND (c.project_id IS NULL OR EXISTS (
    SELECT 1
    FROM projects p
    WHERE p.id = c.project_id
      AND p.organization_id = @organization_id::text
      AND p.deleted IS FALSE
  ));

-- name: GetOrganizationProject :one
SELECT p.id
FROM projects p
WHERE p.id = @id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE;

-- name: CreateAuthClient :one
INSERT INTO mcp_auth_clients (
  organization_id,
  project_id,
  client_type,
  source,
  name,
  created_by_user_id,
  remote_session_client_id,
  client_attachment_scope
) VALUES (
  @organization_id,
  sqlc.narg(project_id)::uuid,
  @client_type,
  'admin',
  @name,
  @created_by_user_id,
  sqlc.narg(remote_session_client_id)::uuid,
  sqlc.narg(client_attachment_scope)::text
)
RETURNING *;

-- name: LockAttachableMCPServer :one
-- A proxied (remote or tunneled) MCP server in the organization, locked so an
-- attachment serializes with SetMcpServerUpstreamAuthRequirement.
SELECT
  ms.id,
  ms.project_id,
  ms.name,
  ms.requires_upstream_auth,
  (ms.remote_mcp_server_id IS NOT NULL OR ms.tunneled_mcp_server_id IS NOT NULL)::boolean AS proxied,
  (ms.tunneled_mcp_server_id IS NOT NULL)::boolean AS tunneled
FROM mcp_servers ms
JOIN projects p
  ON p.id = ms.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
WHERE ms.id = @id
  AND ms.deleted IS FALSE
FOR UPDATE OF ms;

-- name: AttachAuthClientToServer :one
-- Offers the client on the server as an administrator-owned association. An
-- existing live association is kept as it is.
INSERT INTO mcp_server_auth_clients (project_id, organization_id, mcp_server_id, auth_client_id, client_attachment_scope, source)
SELECT ms.project_id, ac.organization_id, ms.id, ac.id, ac.attachment_scope, 'admin'
FROM mcp_auth_clients ac
JOIN mcp_servers ms
  ON ms.id = @mcp_server_id
 AND ms.deleted IS FALSE
 AND (ac.project_id IS NULL OR ac.project_id = ms.project_id)
JOIN projects p
  ON p.id = ms.project_id
 AND p.organization_id = ac.organization_id
 AND p.deleted IS FALSE
WHERE ac.id = @auth_client_id
  AND ac.organization_id = @organization_id
  AND ac.deleted IS FALSE
ON CONFLICT (mcp_server_id, auth_client_id) WHERE deleted IS FALSE DO NOTHING
RETURNING id;

-- name: ListAuthClientStoredHeaders :many
-- A client's live headers with their stored values, for a writer that keeps a
-- secret's stored value when no new one is given. Values stay encrypted.
SELECT h.name,
       h.is_secret,
       h.value
FROM mcp_auth_client_headers h
WHERE h.auth_client_id = @auth_client_id
  AND h.organization_id = @organization_id
  AND h.deleted IS FALSE;

-- name: DeleteAuthClientHeaders :exec
UPDATE mcp_auth_client_headers
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE auth_client_id = @auth_client_id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: CreateAuthClientHeader :exec
INSERT INTO mcp_auth_client_headers (
  auth_client_id,
  organization_id,
  name,
  description,
  is_required,
  is_secret,
  value,
  value_from_request_header
) VALUES (
  @auth_client_id,
  @organization_id,
  @name,
  @description,
  @is_required,
  @is_secret,
  @value,
  @value_from_request_header
);

-- name: GetOrganizationUserSessionIssuer :one
-- A sign-in in the organization: an organization issuer, or one of a live
-- project's.
SELECT usi.id,
       usi.project_id
FROM user_session_issuers AS usi
LEFT JOIN projects AS p
  ON p.id = usi.project_id
WHERE usi.id = @id
  AND usi.deleted IS FALSE
  AND (
    (usi.project_id IS NULL AND usi.organization_id = @organization_id::text)
    OR (p.organization_id = @organization_id::text AND p.deleted IS FALSE)
  );

-- name: IsUserSessionClientOfIssuer :one
SELECT EXISTS (
  SELECT 1
  FROM user_session_clients AS c
  WHERE c.id = @id
    AND c.user_session_issuer_id = @user_session_issuer_id
    AND c.deleted IS FALSE
)::boolean AS is_client;

-- name: IsOrganizationWorkloadIssuer :one
SELECT EXISTS (
  SELECT 1
  FROM workload_issuers AS w
  WHERE w.id = @id
    AND w.organization_id = @organization_id
    AND w.deleted IS FALSE
)::boolean AS is_issuer;

-- name: LockIssuanceProfileForCaller :one
SELECT p.*
FROM issuance_selection_profiles AS p
WHERE p.user_session_issuer_id = @user_session_issuer_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
  AND (
    (sqlc.narg('user_session_client_id')::uuid IS NOT NULL AND p.user_session_client_id = sqlc.narg('user_session_client_id')::uuid)
    OR (sqlc.narg('workload_issuer_id')::uuid IS NOT NULL
        AND p.workload_issuer_id = sqlc.narg('workload_issuer_id')::uuid
        AND p.workload_subject = sqlc.narg('workload_subject')::text)
  )
FOR UPDATE OF p;

-- name: CreateIssuanceProfile :one
INSERT INTO issuance_selection_profiles (
  organization_id,
  project_id,
  user_session_issuer_id,
  user_session_client_id,
  workload_issuer_id,
  workload_subject,
  name,
  created_by_user_id
) VALUES (
  @organization_id,
  sqlc.narg('project_id')::uuid,
  @user_session_issuer_id,
  sqlc.narg('user_session_client_id')::uuid,
  sqlc.narg('workload_issuer_id')::uuid,
  sqlc.narg('workload_subject')::text,
  @name,
  @created_by_user_id
)
RETURNING *;

-- name: RenameIssuanceProfile :one
UPDATE issuance_selection_profiles
SET name = @name,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: LockIssuanceProfile :one
SELECT p.*
FROM issuance_selection_profiles AS p
WHERE p.id = @id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
FOR UPDATE OF p;

-- name: DeleteIssuanceProfile :exec
UPDATE issuance_selection_profiles
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: ListIssuanceProfiles :many
SELECT p.*
FROM issuance_selection_profiles AS p
WHERE p.user_session_issuer_id = @user_session_issuer_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
ORDER BY p.name, p.id;

-- name: ListIssuanceProfileChoices :many
SELECT sel.credential_id AS profile_id,
       sel.mcp_server_id,
       sel.auth_client_id
FROM upstream_client_selections AS sel
WHERE sel.credential_kind = 'issuance_profile'
  AND sel.credential_id = ANY(@profile_ids::uuid[])
  AND sel.organization_id = @organization_id
  AND sel.deleted IS FALSE
ORDER BY sel.credential_id, sel.mcp_server_id;

-- name: ClearIssuanceProfileChoices :exec
UPDATE upstream_client_selections
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE credential_kind = 'issuance_profile'
  AND credential_id = @profile_id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

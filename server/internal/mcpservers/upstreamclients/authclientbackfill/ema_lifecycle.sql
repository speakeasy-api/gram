-- name: ListEMARegistrationBindings :many
-- The organization's ready, client-bound identity chaining bindings in live
-- projects, with the exact resource used for grouping and stable typed keys.
-- in_reach reports that the binding's registration and its issuer are live
-- and belong to the binding's project, its organization or the platform.
SELECT b.id AS binding_id,
       b.project_id,
       b.user_session_issuer_id,
       b.remote_session_issuer_id,
       b.resource AS exact_resource,
       b.resource::text AS resource,
       b.requested_scopes,
       b.remote_session_client_id::uuid AS remote_session_client_id,
       rsi.name AS issuer_name,
       (rsi.id IS NOT NULL AND EXISTS (
         SELECT 1 FROM remote_session_clients AS c
         WHERE c.id = b.remote_session_client_id
           AND c.remote_session_issuer_id = b.remote_session_issuer_id
           AND c.deleted IS FALSE
           AND (c.project_id = b.project_id
                OR (c.project_id IS NULL AND c.organization_id = b.organization_id)
                OR (c.project_id IS NULL AND c.organization_id IS NULL))
       ))::boolean AS in_reach
FROM remote_session_ema_bindings AS b
JOIN projects AS p
  ON p.id = b.project_id
 AND p.organization_id = b.organization_id
 AND p.deleted IS FALSE
LEFT JOIN remote_session_issuers AS rsi
  ON rsi.id = b.remote_session_issuer_id
 AND rsi.deleted IS FALSE
 AND (rsi.project_id = b.project_id
      OR (rsi.project_id IS NULL AND rsi.organization_id = b.organization_id)
      OR (rsi.project_id IS NULL AND rsi.organization_id IS NULL))
WHERE b.organization_id = @organization_id::text
  AND b.state = 'ready'
  AND b.remote_session_client_id IS NOT NULL
  AND b.remote_session_client_id = ANY(@registration_ids::uuid[])
ORDER BY b.id;

-- name: RetireEMARegistrationClient :execrows
WITH retired AS (
  UPDATE mcp_auth_clients AS ac
  SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
  WHERE ac.id = @id::uuid
    AND ac.organization_id = @organization_id::text
    AND ac.source = 'backfill'
    AND ac.client_type = 'ema' AND ac.enabled IS TRUE
    AND ac.deleted IS FALSE
  RETURNING ac.id
), details AS (
  UPDATE mcp_auth_client_emas AS e
  SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
  FROM retired
  WHERE e.auth_client_id = retired.id
    AND e.organization_id = @organization_id::text
    AND e.deleted IS FALSE
  RETURNING e.auth_client_id
), headers AS (
  UPDATE mcp_auth_client_headers AS h
  SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
  FROM retired
  WHERE h.auth_client_id = retired.id
    AND h.organization_id = @organization_id::text
    AND h.deleted IS FALSE
  RETURNING h.id
)
SELECT id FROM retired;

-- name: ListEMARegistrationClients :many
SELECT ac.id FROM mcp_auth_clients ac
JOIN mcp_auth_client_emas e ON e.auth_client_id = ac.id AND e.organization_id = ac.organization_id
WHERE ac.organization_id = @organization_id::text
  AND e.remote_session_client_id = ANY(@registration_ids::uuid[])
  AND ac.source = 'backfill' AND ac.enabled IS TRUE
ORDER BY ac.id;

-- name: ListEMAServerRegistrations :many
-- Candidate registrations are limited to written servers' projects or their old links.
SELECT DISTINCT b.remote_session_client_id::uuid AS registration_id
FROM remote_session_ema_bindings b
JOIN mcp_servers s ON s.project_id = b.project_id
WHERE b.organization_id = @organization_id::text
  AND b.remote_session_client_id IS NOT NULL
  AND s.id = ANY(@server_ids::uuid[])
UNION
SELECT e.remote_session_client_id FROM mcp_auth_client_emas e
JOIN mcp_server_auth_clients l ON l.auth_client_id = e.auth_client_id AND l.organization_id = e.organization_id
WHERE e.organization_id = @organization_id::text AND l.mcp_server_id = ANY(@server_ids::uuid[])
ORDER BY registration_id;

-- name: ListEMASourceServers :many
SELECT s.id FROM mcp_servers s
JOIN projects p ON p.id = s.project_id
WHERE p.organization_id = @organization_id::text
  AND (s.remote_mcp_server_id = ANY(@source_ids::uuid[]) OR s.tunneled_mcp_server_id = ANY(@source_ids::uuid[]))
ORDER BY s.id;

-- name: LockEMALifecycleLinks :exec
SELECT l.id FROM mcp_server_auth_clients l
WHERE l.organization_id = @organization_id::text
  AND l.auth_client_id = ANY(@auth_client_ids::uuid[])
ORDER BY l.id
FOR UPDATE OF l;

-- name: LockEMALifecycleRegistrations :exec
SELECT c.id FROM remote_session_clients c
WHERE c.id = ANY(@registration_ids::uuid[])
  AND (c.organization_id = @organization_id::text OR c.organization_id IS NULL)
  AND (c.project_id IS NULL OR EXISTS (SELECT 1 FROM projects p WHERE p.id=c.project_id AND p.organization_id= @organization_id::text))
ORDER BY c.id FOR NO KEY UPDATE OF c;

-- name: ListEMAWriterLockScope :many
WITH registrations AS (
 SELECT b.remote_session_client_id AS id FROM remote_session_ema_bindings b
 WHERE b.organization_id= @organization_id::text AND b.project_id=ANY(@project_ids::uuid[])
 UNION
 SELECT e.remote_session_client_id FROM mcp_auth_client_emas e
 JOIN mcp_server_auth_clients l ON l.auth_client_id=e.auth_client_id AND l.organization_id=e.organization_id
 WHERE e.organization_id= @organization_id::text AND l.project_id=ANY(@project_ids::uuid[])
), scope_projects AS (
 SELECT p.id FROM projects p WHERE p.organization_id= @organization_id::text AND p.id=ANY(@project_ids::uuid[])
 UNION
 SELECT b.project_id FROM remote_session_ema_bindings b WHERE b.organization_id= @organization_id::text AND b.remote_session_client_id IN (SELECT id FROM registrations)
 UNION
 SELECT l.project_id FROM mcp_server_auth_clients l
 JOIN mcp_auth_client_emas e ON e.auth_client_id=l.auth_client_id AND e.organization_id=l.organization_id
 WHERE e.organization_id= @organization_id::text AND e.remote_session_client_id IN (SELECT id FROM registrations)
)
SELECT 'project'::text AS kind, p.id FROM scope_projects p
UNION
SELECT 'remote'::text AS kind, r.id FROM remote_mcp_servers r WHERE r.project_id IN (SELECT id FROM scope_projects)
UNION
SELECT 'tunneled'::text AS kind, t.id FROM tunneled_mcp_servers t WHERE t.project_id IN (SELECT id FROM scope_projects)
UNION
SELECT 'server'::text AS kind, s.id FROM mcp_servers s WHERE s.project_id IN (SELECT id FROM scope_projects)
ORDER BY kind,id;

-- name: ListEMAGatewayProjects :many
SELECT DISTINCT b.project_id FROM remote_session_ema_bindings b
JOIN user_session_issuers i ON i.id=b.user_session_issuer_id AND i.organization_id=b.organization_id
WHERE b.organization_id= @organization_id::text AND i.project_id IS NULL
ORDER BY b.project_id;

-- name: ListEMAProjectServers :many
SELECT s.id FROM mcp_servers s JOIN projects p ON p.id=s.project_id
WHERE p.organization_id= @organization_id::text AND p.id=ANY(@project_ids::uuid[])
ORDER BY s.id;

-- name: LockEMALifecycleClients :exec
SELECT ac.id FROM mcp_auth_clients ac
WHERE ac.organization_id= @organization_id::text AND ac.id=ANY(@auth_client_ids::uuid[])
ORDER BY ac.id FOR UPDATE OF ac;

-- name: ListEMAFormerServerRegistrations :many
SELECT DISTINCT e.remote_session_client_id FROM mcp_auth_client_emas e
JOIN mcp_server_auth_clients l ON l.auth_client_id = e.auth_client_id AND l.organization_id = e.organization_id
WHERE e.organization_id = @organization_id::text AND l.mcp_server_id = ANY(@server_ids::uuid[])
ORDER BY e.remote_session_client_id;

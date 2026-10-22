-- name: ListClientScopeCandidates :many
-- Live clients created before the cutoff that still store a scope, with the
-- evidence the runner needs to decide whether the scope is a copy taken at
-- creation: equal as a set to the issuer's or a bound server's resource
-- scopes_supported, and never changed by an audited edit. An update entry
-- without both snapshots counts as an edit, since it cannot prove otherwise.
SELECT
  c.id,
  c.project_id,
  COALESCE(c.organization_id, p.organization_id, '')::text AS organization_id,
  c.client_id,
  c.scope,
  c.updated_at,
  (c.identity_provider_connection_id IS NOT NULL)::boolean AS managed,
  (c.credential_owner <> 'subject' OR c.legacy_callback_url)::boolean AS excluded_kind,
  COALESCE(
    (SELECT array_agg(DISTINCT s ORDER BY s) FROM unnest(c.scope) AS s)
      = (SELECT array_agg(DISTINCT s ORDER BY s) FROM unnest(i.scopes_supported) AS s),
    FALSE
  )::boolean AS matches_issuer,
  EXISTS (
    SELECT 1
    FROM remote_session_client_user_session_issuers AS link
    JOIN mcp_servers AS m ON m.user_session_issuer_id = link.user_session_issuer_id AND m.deleted IS FALSE
    JOIN projects AS mp ON mp.id = m.project_id
    JOIN remote_mcp_servers AS rms ON rms.id = m.remote_mcp_server_id AND rms.project_id = m.project_id AND rms.deleted IS FALSE
    JOIN remote_protected_resources AS r ON r.project_id = m.project_id AND r.organization_id = mp.organization_id AND r.resource_identifier = rms.url AND r.deleted IS FALSE
    WHERE link.remote_session_client_id = c.id
      AND (m.project_id = c.project_id OR (c.project_id IS NULL AND mp.organization_id = c.organization_id))
      AND (SELECT array_agg(DISTINCT s ORDER BY s) FROM unnest(c.scope) AS s)
        = (SELECT array_agg(DISTINCT s ORDER BY s) FROM unnest(r.scopes_supported) AS s)
  )::boolean AS matches_resource,
  EXISTS (
    SELECT 1
    FROM audit_logs AS l
    WHERE l.organization_id = COALESCE(c.organization_id, p.organization_id)
      AND l.subject_type = 'remote_session_client'
      AND l.subject_id = c.id::text
      AND l.action = 'remote-session-client:update'
      AND (
        l.before_snapshot IS NULL
        OR l.after_snapshot IS NULL
        OR l.before_snapshot->'Scope' IS DISTINCT FROM l.after_snapshot->'Scope'
      )
  )::boolean AS scope_edited
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id
LEFT JOIN projects AS p ON p.id = c.project_id
WHERE c.deleted IS FALSE
  AND cardinality(c.scope) > 0
  AND c.created_at < @created_before
  AND c.id > @after_id
  AND (sqlc.narg('client_id')::uuid IS NULL OR c.id = sqlc.narg('client_id')::uuid)
ORDER BY c.id ASC
LIMIT @page_size;

-- name: LockClientForScopeClear :one
SELECT *
FROM remote_session_clients
WHERE id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- name: ClearClientScope :one
-- Compare-and-swap on the updated_at read under the lock.
UPDATE remote_session_clients
SET scope = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND updated_at = @expected_updated_at
  AND deleted IS FALSE
  AND identity_provider_connection_id IS NULL
RETURNING *;

-- name: ListUserSessionIssuerIDsForClient :many
SELECT link.user_session_issuer_id
FROM remote_session_client_user_session_issuers AS link
WHERE link.remote_session_client_id = @remote_session_client_id
ORDER BY link.user_session_issuer_id;

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug)
VALUES (@id, @id, @id);

-- name: SeedProjectFixture :one
INSERT INTO projects (name, slug, organization_id)
VALUES (@slug, @slug, @organization_id)
RETURNING id;

-- name: SeedIssuerFixture :one
INSERT INTO remote_session_issuers (project_id, slug, issuer, scopes_supported)
VALUES (@project_id, @slug, @issuer, @scopes_supported)
RETURNING id;

-- name: SeedClientFixture :one
INSERT INTO remote_session_clients (project_id, organization_id, remote_session_issuer_id, client_id, scope, created_at, updated_at)
VALUES (@project_id, @organization_id, @remote_session_issuer_id, @client_id, sqlc.narg('scope')::text[], @created_at, @created_at)
RETURNING id;

-- name: SeedBoundResourceFixture :exec
WITH usi AS (
  INSERT INTO user_session_issuers (project_id, slug, authn_challenge_mode, session_duration)
  VALUES (@project_id, @slug, 'interactive', interval '1 hour')
  RETURNING id
), link AS (
  INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
  SELECT @client_id, usi.id FROM usi
), rms AS (
  INSERT INTO remote_mcp_servers (project_id, transport_type, url)
  VALUES (@project_id, 'streamable-http', @url)
  RETURNING id
), srv AS (
  INSERT INTO mcp_servers (project_id, user_session_issuer_id, remote_mcp_server_id, visibility)
  SELECT @project_id, usi.id, rms.id, 'private' FROM usi, rms
)
INSERT INTO remote_protected_resources (project_id, organization_id, resource_identifier, scopes_supported)
VALUES (@project_id, @organization_id, @url, @scopes_supported);

-- name: SeedClientUpdateAuditFixture :exec
INSERT INTO audit_logs (organization_id, project_id, actor_id, actor_type, action, subject_id, subject_type, before_snapshot, after_snapshot)
VALUES (@organization_id, @project_id, 'user_fixture', 'user', 'remote-session-client:update', @subject_id, 'remote_session_client', @before_snapshot, @after_snapshot);

-- name: GetClientScopeFixture :one
SELECT scope FROM remote_session_clients WHERE id = @id;

-- name: ListClientAuditActionsFixture :many
SELECT action, actor_type, actor_id, before_snapshot, after_snapshot
FROM audit_logs
WHERE subject_id = @subject_id
ORDER BY seq ASC;

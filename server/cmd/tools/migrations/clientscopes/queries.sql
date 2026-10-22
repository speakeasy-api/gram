-- name: ListClientScopeCandidates :many
-- Live clients created before the cutoff that store a scope; an update audit missing a snapshot counts as a scope edit.
SELECT
  c.id,
  c.project_id,
  c.organization_id AS client_organization_id,
  COALESCE(c.organization_id, p.organization_id, '')::text AS organization_id,
  c.client_id,
  c.scope,
  i.scopes_supported AS issuer_scopes_supported,
  i.scope_override AS issuer_scope_override,
  COALESCE(i.omit_scope_fallback, FALSE)::boolean AS issuer_omit_scope_fallback,
  (c.identity_provider_connection_id IS NOT NULL)::boolean AS managed,
  (c.credential_owner <> 'subject' OR c.legacy_callback_url)::boolean AS excluded_kind,
  EXISTS (
    SELECT 1 FROM user_session_issuers AS usi
    WHERE usi.trusted_remote_session_client_id = c.id AND usi.deleted IS FALSE
  )::boolean AS trusted_sign_in,
  EXISTS (
    SELECT 1 FROM remote_session_ema_bindings AS b
    WHERE b.remote_session_client_id = c.id
      AND b.state IS DISTINCT FROM 'unlinked'
  )::boolean AS identity_chaining,
  EXISTS (
    SELECT 1
    FROM remote_session_client_user_session_issuers AS link
    JOIN user_session_issuers AS usi ON usi.id = link.user_session_issuer_id AND usi.deleted IS FALSE
    WHERE link.remote_session_client_id = c.id
  )::boolean AS linked,
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
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id AND i.deleted IS FALSE
LEFT JOIN projects AS p ON p.id = c.project_id
WHERE c.deleted IS FALSE
  AND p.deleted IS NOT TRUE
  AND (
    i.project_id = c.project_id
    OR (i.project_id IS NULL AND i.organization_id IS NULL)
    OR (i.project_id IS NULL AND i.organization_id = CASE WHEN c.project_id IS NULL THEN c.organization_id ELSE p.organization_id END)
  )
  AND cardinality(c.scope) > 0
  AND c.created_at < @created_before
  AND c.id > @after_id
  AND (sqlc.narg('client_id')::uuid IS NULL OR c.id = sqlc.narg('client_id')::uuid)
ORDER BY c.id ASC
LIMIT @page_size;

-- name: ListClientLoginServers :many
-- The servers that sign in through the client's user session issuers, in its tenant; url is empty when not remote-backed.
SELECT
  link.user_session_issuer_id,
  m.project_id,
  COALESCE(rms.url, '')::text AS url
FROM remote_session_client_user_session_issuers AS link
JOIN user_session_issuers AS usi ON usi.id = link.user_session_issuer_id AND usi.deleted IS FALSE
JOIN mcp_servers AS m ON m.user_session_issuer_id = usi.id AND m.deleted IS FALSE
JOIN projects AS mp ON mp.id = m.project_id AND mp.deleted IS FALSE
LEFT JOIN remote_mcp_servers AS rms ON rms.id = m.remote_mcp_server_id AND rms.project_id = m.project_id AND rms.deleted IS FALSE
WHERE link.remote_session_client_id = @client_id
  AND mp.organization_id = @organization_id
  AND (sqlc.narg('project_id')::uuid IS NULL OR m.project_id = sqlc.narg('project_id')::uuid)
ORDER BY m.id ASC;

-- name: ListClientMemberServers :many
-- Servers the client is attached to directly, which gateway member logins read with it; url is empty when not remote-backed.
SELECT
  x.project_id,
  COALESCE(rms.url, '')::text AS url
FROM mcp_server_remote_session_clients AS x
JOIN mcp_servers AS m ON m.id = x.mcp_server_id AND m.project_id = x.project_id AND m.deleted IS FALSE
JOIN projects AS mp ON mp.id = x.project_id AND mp.deleted IS FALSE
LEFT JOIN remote_mcp_servers AS rms ON rms.id = m.remote_mcp_server_id AND rms.project_id = m.project_id AND rms.deleted IS FALSE
WHERE x.remote_session_client_id = @client_id
  AND x.organization_id = @organization_id
  AND x.deleted IS FALSE
  AND (sqlc.narg('project_id')::uuid IS NULL OR x.project_id = sqlc.narg('project_id')::uuid)
ORDER BY m.id ASC;

-- name: AttachMemberClientFixture :exec
WITH rms AS (
  INSERT INTO remote_mcp_servers (project_id, transport_type, url)
  VALUES (@project_id, 'streamable-http', @url)
  RETURNING id
), srv AS (
  INSERT INTO mcp_servers (project_id, remote_mcp_server_id, visibility)
  SELECT @project_id, rms.id, 'private' FROM rms
  RETURNING id, project_id
), attach AS (
  INSERT INTO mcp_server_remote_session_clients (project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope)
  SELECT srv.project_id, @organization_id, srv.id, @client_id, 'project:' || srv.project_id::text FROM srv
)
INSERT INTO remote_protected_resources (project_id, organization_id, resource_identifier, scopes_supported, scope_override, metadata_fetched_at)
VALUES (@project_id, @organization_id, @url, @scopes_supported, sqlc.narg('scope_override')::text[], clock_timestamp());

-- name: LockClientForScopeWrite :one
SELECT *
FROM remote_session_clients
WHERE id = @id
  AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')::uuid
  AND (organization_id IS NULL OR organization_id = @organization_id::text)
  AND deleted IS FALSE
FOR UPDATE;

-- name: ClearClientScope :one
UPDATE remote_session_clients
SET scope = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND updated_at = @expected_updated_at
  AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')::uuid
  AND (organization_id IS NULL OR organization_id = @organization_id::text)
  AND deleted IS FALSE
  AND identity_provider_connection_id IS NULL
RETURNING *;

-- name: ListBackfillClears :many
-- Every clear this backfill audited, with the scope it cleared in its original order.
SELECT
  l.organization_id,
  l.project_id,
  l.subject_id::uuid AS client_id,
  ARRAY(
    SELECT s.value
    FROM jsonb_array_elements_text(l.before_snapshot->'Scope') WITH ORDINALITY AS s(value, n)
    ORDER BY s.n
  )::text[] AS scope
FROM audit_logs AS l
WHERE l.actor_type = 'system'
  AND l.actor_id = @actor_id
  AND l.action = 'remote-session-client:update'
  AND l.subject_type = 'remote_session_client'
  AND jsonb_typeof(l.before_snapshot->'Scope') = 'array'
  AND jsonb_typeof(l.after_snapshot->'Scope') IS DISTINCT FROM 'array'
ORDER BY l.seq ASC;

-- name: RestoreClientScope :one
UPDATE remote_session_clients
SET scope = @scope::text[],
    updated_at = clock_timestamp()
WHERE id = @id
  AND updated_at = @expected_updated_at
  AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')::uuid
  AND (organization_id IS NULL OR organization_id = @organization_id::text)
  AND scope IS NULL
  AND deleted IS FALSE
  AND identity_provider_connection_id IS NULL
RETURNING *;

-- name: SeedOrganizationFixture :exec
INSERT INTO organization_metadata (id, name, slug)
VALUES (@id, @id, @id);

-- name: SeedProjectFixture :one
INSERT INTO projects (name, slug, organization_id)
VALUES (@slug, @slug, @organization_id)
RETURNING id;

-- name: SeedIssuerFixture :one
INSERT INTO remote_session_issuers (project_id, organization_id, slug, issuer, scopes_supported)
VALUES (sqlc.narg('project_id')::uuid, sqlc.narg('organization_id')::text, @slug, @issuer, @scopes_supported)
RETURNING id;

-- name: SeedClientFixture :one
INSERT INTO remote_session_clients (project_id, organization_id, remote_session_issuer_id, client_id, scope, legacy_callback_url, created_at, updated_at)
VALUES (sqlc.narg('project_id')::uuid, @organization_id, @remote_session_issuer_id, @client_id, sqlc.narg('scope')::text[], @legacy_callback_url, @created_at, @created_at)
RETURNING id;

-- name: SeedUserSessionIssuerFixture :one
INSERT INTO user_session_issuers (project_id, slug, authn_challenge_mode, session_duration)
VALUES (@project_id, @slug, 'interactive', interval '1 hour')
RETURNING id;

-- name: LinkClientFixture :exec
INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id, user_session_issuer_id)
VALUES (@client_id, @user_session_issuer_id);

-- name: TrustClientFixture :exec
UPDATE user_session_issuers SET trusted_remote_session_client_id = @client_id WHERE id = @id;

-- name: SetResourcePinFixture :exec
UPDATE remote_protected_resources SET scope_override = @scope_override WHERE project_id = @project_id;

-- name: SoftDeleteClientFixture :exec
UPDATE remote_session_clients SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: SeedServerResourceFixture :exec
WITH rms AS (
  INSERT INTO remote_mcp_servers (project_id, transport_type, url)
  VALUES (@project_id, 'streamable-http', @url)
  RETURNING id
), srv AS (
  INSERT INTO mcp_servers (project_id, user_session_issuer_id, remote_mcp_server_id, visibility)
  SELECT @project_id, @user_session_issuer_id, rms.id, 'private' FROM rms
)
INSERT INTO remote_protected_resources (project_id, organization_id, resource_identifier, scopes_supported, metadata_fetched_at)
VALUES (@project_id, @organization_id, @url, @scopes_supported, @metadata_fetched_at);

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

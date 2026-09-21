-- The organization's live Okta connection with the display fields readiness
-- needs. Exactly one live connection per organization per provider.
-- name: GetLiveConnection :one
SELECT
    c.id
  , c.status
  , o.org_url
  , o.agent_id
FROM identity_provider_connections AS c
JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
 AND o.organization_id = c.organization_id
 AND o.deleted IS FALSE
WHERE c.organization_id = @organization_id
  AND c.provider = 'okta'
  AND c.deleted IS FALSE;

-- Held for the duration of a confirm or reset so a concurrent revoke, which
-- takes the row FOR UPDATE, serializes against it. FOR UPDATE rather than
-- FOR SHARE so waiters queue in order: a stream of share locks would starve
-- the revoke indefinitely.
-- name: LockLiveConnection :one
SELECT
    c.id
  , c.status
FROM identity_provider_connections AS c
WHERE c.id = @id
  AND c.organization_id = @organization_id
  AND c.provider = 'okta'
  AND c.deleted IS FALSE
  AND c.status IN ('verified', 'degraded')
FOR UPDATE;

-- Servers the organization owns (tenancy through projects, since mcp_servers
-- has no organization column) with a live backend and an authorization server
-- the organization can see. Whether Speakeasy fronts the user login does not
-- matter: readiness is about the upstream authorization server. Undiscovered
-- issuers are returned so the list can count them; whether the issuer
-- advertises ID-JAG is decided in Go so the derivation stays pure.
-- name: ListEligibleServers :many
SELECT
    ms.id
  , ms.project_id
  , ms.user_session_issuer_id
  , p.slug AS project_slug
  , ms.name
  , ms.slug
  , i.id AS issuer_id
  , i.metadata_fetched_at
  , i.grant_types_supported
  , i.authorization_grant_profiles_supported
  , t.resource_identifier AS tunneled_resource_identifier
  , r.url AS remote_url
  , u.url AS unproxied_url
FROM mcp_servers AS ms
JOIN projects AS p
  ON p.id = ms.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
JOIN remote_session_issuers AS i
  ON i.id = ms.remote_session_issuer_id
 AND i.deleted IS FALSE
 AND (
   i.project_id = ms.project_id
   OR (i.project_id IS NULL AND i.organization_id = @organization_id)
   OR (i.project_id IS NULL AND i.organization_id IS NULL)
 )
LEFT JOIN tunneled_mcp_servers AS t
  ON t.id = ms.tunneled_mcp_server_id
 AND t.project_id = ms.project_id
 AND t.deleted IS FALSE
 AND t.status <> 'revoked'
LEFT JOIN remote_mcp_servers AS r
  ON r.id = ms.remote_mcp_server_id
 AND r.project_id = ms.project_id
 AND r.deleted IS FALSE
LEFT JOIN unproxied_mcp_servers AS u
  ON u.id = ms.unproxied_mcp_server_id
 AND u.project_id = ms.project_id
 AND u.deleted IS FALSE
WHERE ms.deleted IS FALSE
  AND ms.visibility <> 'disabled'
  AND ms.toolset_id IS NULL
  AND (t.id IS NOT NULL OR r.id IS NOT NULL OR u.id IS NOT NULL)
ORDER BY ms.name ASC NULLS LAST, ms.id ASC;

-- One eligible, discovered server by id, re-read inside the write
-- transaction; ids outside the organization do not resolve.
-- name: GetEligibleServer :one
SELECT
    ms.id
  , ms.project_id
  , ms.user_session_issuer_id
  , p.slug AS project_slug
  , ms.name
  , ms.slug
  , i.id AS issuer_id
  , i.metadata_fetched_at
  , i.grant_types_supported
  , i.authorization_grant_profiles_supported
  , t.resource_identifier AS tunneled_resource_identifier
  , r.url AS remote_url
  , u.url AS unproxied_url
FROM mcp_servers AS ms
JOIN projects AS p
  ON p.id = ms.project_id
 AND p.organization_id = @organization_id
 AND p.deleted IS FALSE
JOIN remote_session_issuers AS i
  ON i.id = ms.remote_session_issuer_id
 AND i.deleted IS FALSE
 AND (
   i.project_id = ms.project_id
   OR (i.project_id IS NULL AND i.organization_id = @organization_id)
   OR (i.project_id IS NULL AND i.organization_id IS NULL)
 )
LEFT JOIN tunneled_mcp_servers AS t
  ON t.id = ms.tunneled_mcp_server_id
 AND t.project_id = ms.project_id
 AND t.deleted IS FALSE
 AND t.status <> 'revoked'
LEFT JOIN remote_mcp_servers AS r
  ON r.id = ms.remote_mcp_server_id
 AND r.project_id = ms.project_id
 AND r.deleted IS FALSE
LEFT JOIN unproxied_mcp_servers AS u
  ON u.id = ms.unproxied_mcp_server_id
 AND u.project_id = ms.project_id
 AND u.deleted IS FALSE
WHERE ms.id = @mcp_server_id
  AND ms.deleted IS FALSE
  AND ms.visibility <> 'disabled'
  AND ms.toolset_id IS NULL
  AND (t.id IS NOT NULL OR r.id IS NOT NULL OR u.id IS NOT NULL)
  AND i.metadata_fetched_at IS NOT NULL;

-- Clients the organization holds on the given issuers: project-owned ones in
-- the organization's projects and organization-owned ones.
-- name: ListIssuerClients :many
SELECT
    c.id
  , c.remote_session_issuer_id
  , c.project_id
  , c.client_id
  , c.scope
  , c.resource_identifier
  , i.scope_override AS issuer_scope_override
  , i.scopes_supported AS issuer_scopes_supported
  , (
      SELECT COALESCE(array_agg(link.user_session_issuer_id ORDER BY link.user_session_issuer_id), '{}'::uuid[])
      FROM remote_session_client_user_session_issuers AS link
      JOIN user_session_issuers AS usi ON usi.id = link.user_session_issuer_id
      WHERE link.remote_session_client_id = c.id
        AND usi.deleted IS FALSE
        AND (
          (usi.project_id IS NULL AND usi.organization_id = @organization_id::text)
          OR usi.project_id IN (
            SELECT id FROM projects
            WHERE organization_id = @organization_id::text AND deleted IS FALSE
          )
        )
    )::uuid[] AS user_session_issuer_ids
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id AND i.deleted IS FALSE
LEFT JOIN projects AS p
  ON p.id = c.project_id
WHERE c.deleted IS FALSE
  AND c.remote_session_issuer_id = ANY (@issuer_ids::uuid[])
  AND (
    (c.project_id IS NOT NULL AND p.organization_id = @organization_id AND p.deleted IS FALSE)
    OR (c.project_id IS NULL AND (c.organization_id IS NULL OR c.organization_id = @organization_id))
  )
ORDER BY c.remote_session_issuer_id, c.project_id NULLS LAST, c.created_at, c.id;

-- name: ListEMABindings :many
SELECT
    b.project_id
  , b.user_session_issuer_id
  , b.remote_session_issuer_id
  , b.resource
  , b.remote_session_client_id
  , b.requested_scopes
FROM remote_session_ema_bindings AS b
WHERE b.organization_id = @organization_id
  AND b.remote_session_issuer_id = ANY (@issuer_ids::uuid[])
  AND b.remote_session_client_id IS NOT NULL
ORDER BY b.project_id, b.remote_session_issuer_id, b.resource, b.user_session_issuer_id, b.id;

-- Resource connections of an identity provider connection with the label of the identity provider app
-- instance the administrator picked, when the snapshot still has it.
-- name: ListResourceConnections :many
SELECT
    sqlc.embed(r)
  , a.label AS okta_application_label
FROM okta_resource_connections AS r
LEFT JOIN okta_applications AS a
  ON a.organization_id = r.organization_id
 AND a.identity_provider_connection_id = r.identity_provider_connection_id
 AND a.okta_app_id = r.okta_application_id
 AND a.removed_at IS NULL
WHERE r.organization_id = @organization_id
  AND r.identity_provider_connection_id = @identity_provider_connection_id
ORDER BY r.created_at, r.id;

-- name: HasResourceConnections :one
SELECT EXISTS (
  SELECT 1
  FROM okta_resource_connections
  WHERE organization_id = @organization_id
    AND identity_provider_connection_id = @identity_provider_connection_id
);

-- name: GetOktaApplicationLabel :one
SELECT label
FROM okta_applications
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND okta_app_id = @okta_app_id
  AND removed_at IS NULL;

-- name: GetResourceConnectionForUpdate :one
SELECT *
FROM okta_resource_connections
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND remote_session_issuer_id = @remote_session_issuer_id
  AND resource = @resource
FOR UPDATE;

-- A row is the confirmation. Repeating it updates the audience and keeps the
-- recorded app instance unless a new one is given.
-- name: UpsertResourceConnection :one
INSERT INTO okta_resource_connections (
  organization_id,
  identity_provider_connection_id,
  remote_session_issuer_id,
  resource,
  audience,
  okta_application_id
) VALUES (
  @organization_id,
  @identity_provider_connection_id,
  @remote_session_issuer_id,
  @resource,
  @audience,
  sqlc.narg(okta_application_id)
)
ON CONFLICT (organization_id, identity_provider_connection_id, remote_session_issuer_id, resource) DO UPDATE
SET audience = EXCLUDED.audience,
    okta_application_id = COALESCE(EXCLUDED.okta_application_id, okta_resource_connections.okta_application_id),
    updated_at = clock_timestamp()
RETURNING *;

-- Reset withdraws the confirmation for the upstream; every server sharing it
-- reads as unconfirmed again. Observed evidence goes with the row.
-- name: DeleteResourceConnection :one
DELETE FROM okta_resource_connections
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND remote_session_issuer_id = @remote_session_issuer_id
  AND resource = @resource
RETURNING *;

-- Revocation deletes the connection's resource connections outright.
-- name: DeleteResourceConnectionsForConnection :execrows
DELETE FROM okta_resource_connections
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id;

-- Test fixture: an issuer with discovered metadata advertising the ID-JAG
-- profile and jwt-bearer.
-- name: SetIssuerGrantCapabilitiesFixture :execrows
UPDATE remote_session_issuers
SET grant_types_supported = @grant_types_supported::text[],
    authorization_grant_profiles_supported = @authorization_grant_profiles_supported::text[],
    metadata_fetched_at = clock_timestamp()
WHERE id = @id;

-- Test fixture: a snapshot row for an identity provider app instance.
-- name: CreateOktaApplicationFixture :one
INSERT INTO okta_applications (organization_id, identity_provider_connection_id, okta_app_id, label, name, sign_on_mode, status)
VALUES (@organization_id, @identity_provider_connection_id, @okta_app_id, @label, @name, 'SAML_2_0', 'ACTIVE')
RETURNING id;

-- Test fixture: a remote-backed MCP server bound to an authorization server.
-- name: CreateRemoteBackendFixture :one
INSERT INTO remote_mcp_servers (project_id, name, slug, transport_type, url)
VALUES (@project_id, @name, @slug, 'streamable_http', @url)
RETURNING id;

-- name: CreateTunneledBackendFixture :one
INSERT INTO tunneled_mcp_servers (project_id, name, key_hash, key_prefix, status, resource_identifier)
VALUES (@project_id, @name, @key_hash, @key_prefix, @status, @resource_identifier)
RETURNING id;

-- name: CreateTunneledMCPServerFixture :one
INSERT INTO mcp_servers (project_id, name, slug, tunneled_mcp_server_id, remote_session_issuer_id, visibility)
VALUES (@project_id, @name, @slug, @tunneled_mcp_server_id, @remote_session_issuer_id, 'private')
RETURNING id;

-- name: CreateEligibleMCPServerFixture :one
INSERT INTO mcp_servers (project_id, name, slug, remote_mcp_server_id, remote_session_issuer_id, user_session_issuer_id, visibility)
VALUES (@project_id, @name, @slug, @remote_mcp_server_id, @remote_session_issuer_id, sqlc.narg(user_session_issuer_id), @visibility)
RETURNING id;

-- name: SetMCPServerVisibilityFixture :execrows
UPDATE mcp_servers
SET visibility = @visibility
WHERE id = @id
  AND project_id = @project_id;

-- name: SetMCPServerIssuerFixture :execrows
UPDATE mcp_servers
SET remote_session_issuer_id = @remote_session_issuer_id
WHERE id = @id
  AND project_id = @project_id;

-- name: SoftDeleteRemoteBackendFixture :execrows
UPDATE remote_mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id;

-- Test fixture: a client registered at an authorization server.
-- name: CreateIssuerClientFixture :one
INSERT INTO remote_session_clients (project_id, organization_id, remote_session_issuer_id, client_id, scope, resource_identifier)
VALUES (sqlc.narg(project_id), sqlc.narg(organization_id), @remote_session_issuer_id, @client_id, @scope::text[], sqlc.narg(resource_identifier))
RETURNING id;

-- name: GetIssuerFixture :one
SELECT id, issuer, attachment_scope
FROM remote_session_issuers
WHERE id = @id;

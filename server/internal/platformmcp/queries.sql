-- name: CreateAdminMCPOAuthClient :one
INSERT INTO admin_mcp_oauth_clients (
    client_id,
    client_secret_hash,
    client_name,
    redirect_uris,
    client_secret_expires_at
) VALUES (
    @client_id,
    @client_secret_hash,
    @client_name,
    @redirect_uris,
    @client_secret_expires_at
)
RETURNING *;

-- name: GetActiveAdminMCPOAuthClientByClientID :one
SELECT *
FROM admin_mcp_oauth_clients
WHERE client_id = @client_id
  AND revoked_at IS NULL;

-- name: RevokeAdminMCPOAuthClient :one
UPDATE admin_mcp_oauth_clients
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE client_id = @client_id
  AND revoked_at IS NULL
RETURNING id;

-- name: RevokeAdminMCPConnectionsForClient :many
UPDATE admin_mcp_connections
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL
RETURNING id, active_generation;

-- name: LockAdminMCPConnectionAuthorization :exec
SELECT pg_advisory_xact_lock(hashtext(@organization_id || ':' || @subject_urn || ':' || @oauth_client_id::text));

-- name: CreateAdminMCPConnection :one
INSERT INTO admin_mcp_connections (
    id,
    organization_id,
    subject_urn,
    oauth_client_id,
    active_generation
) VALUES (
    @id,
    @organization_id,
    @subject_urn,
    @oauth_client_id,
    @active_generation
)
RETURNING *;

-- name: GetActiveAdminMCPConnection :one
SELECT *
FROM admin_mcp_connections
WHERE organization_id = @organization_id
  AND subject_urn = @subject_urn
  AND oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL;

-- name: GetActiveAdminMCPConnectionByID :one
SELECT connection.*, client.client_id
FROM admin_mcp_connections AS connection
JOIN admin_mcp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
WHERE connection.id = @id
  AND connection.revoked_at IS NULL
  AND client.revoked_at IS NULL;

-- name: GetAdminMCPConnectionForUpdate :one
SELECT connection.*, client.client_id, client.revoked_at AS client_revoked_at
FROM admin_mcp_connections AS connection
JOIN admin_mcp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
WHERE connection.id = @id
FOR UPDATE OF connection;

-- name: RevokeAdminMCPConnection :one
UPDATE admin_mcp_connections
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE id = @id
  AND revoked_at IS NULL
RETURNING *;

-- name: RotateAdminMCPConnectionGeneration :one
UPDATE admin_mcp_connections
SET active_generation = @active_generation,
    reauthorized_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @connection_id
  AND revoked_at IS NULL
RETURNING *;

-- name: CreateAdminMCPAuthorizationGrant :one
INSERT INTO admin_mcp_authorization_grants (
    authorization_code_hash,
    oauth_client_id,
    connection_id,
    connection_generation,
    redirect_uri,
    code_challenge,
    expires_at
) VALUES (
    @authorization_code_hash,
    @oauth_client_id,
    @connection_id,
    @connection_generation,
    @redirect_uri,
    @code_challenge,
    @expires_at
)
RETURNING *;

-- name: GetAdminMCPAuthorizationGrantForConsume :one
SELECT auth_grant.*, connection.organization_id, connection.subject_urn, connection.active_generation, client.client_id
FROM admin_mcp_authorization_grants AS auth_grant
JOIN admin_mcp_connections AS connection
  ON connection.id = auth_grant.connection_id
 AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN admin_mcp_oauth_clients AS client
  ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.authorization_code_hash = @authorization_code_hash
  AND connection.revoked_at IS NULL
  AND client.revoked_at IS NULL
FOR UPDATE OF auth_grant;

-- name: ConsumeAdminMCPAuthorizationGrant :one
UPDATE admin_mcp_authorization_grants
SET consumed_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND consumed_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: CreateAdminMCPSession :one
INSERT INTO admin_mcp_sessions (
    id,
    connection_id,
    oauth_client_id,
    connection_generation,
    jti,
    refresh_token_hash,
    expires_at,
    refresh_expires_at
) VALUES (
    @id,
    @connection_id,
    @oauth_client_id,
    @connection_generation,
    @jti,
    @refresh_token_hash,
    @expires_at,
    @refresh_expires_at
)
RETURNING *;

-- name: GetAdminMCPSessionForRefresh :one
SELECT session.*, connection.organization_id, connection.subject_urn, connection.active_generation, client.client_id
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id
 AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client
  ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = @refresh_token_hash;

-- name: GetAdminMCPSessionForRefreshForUpdate :one
SELECT session.*, connection.organization_id, connection.subject_urn, connection.active_generation, client.client_id
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id
 AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client
  ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = @refresh_token_hash
FOR UPDATE OF session;

-- name: RotateAdminMCPSession :one
UPDATE admin_mcp_sessions
SET revoked_at = @rotated_at,
    rotated_at = @rotated_at,
    replaced_by_session_id = @replaced_by_session_id,
    updated_at = @rotated_at
WHERE id = @id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAdminMCPSession :one
UPDATE admin_mcp_sessions
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAdminMCPSessionByJTI :one
UPDATE admin_mcp_sessions
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE jti = @jti
  AND oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAdminMCPSessionFamily :exec
UPDATE admin_mcp_sessions
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE connection_id = @connection_id
  AND connection_generation = @connection_generation
  AND revoked_at IS NULL;

-- name: RevokeAdminMCPSessionsForClient :exec
UPDATE admin_mcp_sessions
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL;

-- name: GetActiveAdminMCPSessionByJTI :one
SELECT
    session.connection_id,
    session.oauth_client_id,
    session.connection_generation,
    connection.organization_id,
    connection.subject_urn,
    connection.active_generation,
    client.client_id
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id
 AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client
  ON client.id = session.oauth_client_id
WHERE session.jti = @jti
  AND session.expires_at > clock_timestamp()
  AND session.revoked_at IS NULL
  AND connection.revoked_at IS NULL
  AND connection.active_generation = session.connection_generation
  AND client.revoked_at IS NULL;

-- name: ListAdminMCPProjects :many
SELECT id, name, slug
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY id ASC
LIMIT @limit_value;

-- name: ListAdminMCPServers :many
SELECT server.id, server.project_id, server.name, server.slug, server.visibility
FROM mcp_servers AS server
JOIN projects
  ON projects.id = server.project_id
WHERE server.project_id = @project_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND server.deleted IS FALSE
ORDER BY server.id ASC
LIMIT @limit_value;

-- name: GetAdminMCPServer :one
SELECT server.id, server.project_id, server.name, server.slug, server.visibility
FROM mcp_servers AS server
JOIN projects
  ON projects.id = server.project_id
WHERE server.id = @mcp_server_id
  AND server.project_id = @project_id
  AND projects.organization_id = @organization_id
  AND projects.deleted IS FALSE
  AND server.deleted IS FALSE;

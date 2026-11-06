-- name: RecordWriteEvent :exec
-- Staff-only write trail. Stores the operation, its target and bounded reason
-- codes; never arguments or results.
INSERT INTO admin_mcp_write_events (subject_urn, oauth_client_id, event, reason_code, operation, organization_id, resource_kind, resource_id)
VALUES (@subject_urn, @oauth_client_id, @event, @reason_code, @operation, @organization_id, @resource_kind, @resource_id);

-- name: LockLiveOAuthClient :one
-- Consent takes the client row before the connection row. Writes use
-- the same order and never takes an exclusive lock on a client shared by other
-- staff.
SELECT id
FROM admin_mcp_oauth_clients
WHERE id = @id
  AND revoked_at IS NULL
  AND (client_secret_expires_at IS NULL OR client_secret_expires_at > clock_timestamp())
FOR SHARE;

-- name: LockWriteConnectionShared :one
-- Lets concurrent executes on one connection proceed while still blocking a
-- generation change from reconsent or revocation.
SELECT active_generation
FROM admin_mcp_connections
WHERE id = @id
  AND oauth_client_id = @oauth_client_id
  AND subject_urn = @subject_urn
  AND revoked_at IS NULL
  AND reauthorization_required_at IS NULL
  AND authorization_expires_at > clock_timestamp()
  AND scopes @> ARRAY['admin:read', 'admin:write']::text[]
FOR SHARE;

-- name: RegisterOAuthClient :exec
INSERT INTO admin_mcp_oauth_clients (client_id, client_name, client_secret_hash, redirect_uris)
VALUES (@client_id, @client_name, NULLIF(@client_secret_hash::text, ''), @redirect_uris::text[]);

-- name: GetLiveOAuthClient :one
SELECT client_id, client_name, client_secret_hash, redirect_uris, client_secret_expires_at
FROM admin_mcp_oauth_clients
WHERE client_id = @client_id
  AND revoked_at IS NULL;

-- name: LockOAuthClientForConsent :one
-- Consent locks the client row before the connection row. Writes take
-- the same order.
SELECT id
FROM admin_mcp_oauth_clients
WHERE client_id = @client_id
  AND revoked_at IS NULL
  AND (client_secret_expires_at IS NULL OR client_secret_expires_at > clock_timestamp())
FOR UPDATE;

-- name: UpsertStaffConnection :one
-- Reconsent rotates the active generation, invalidating every earlier session
-- and grant for this staff subject and client.
INSERT INTO admin_mcp_connections
  (subject_urn, oauth_client_id, admin_session_id_enc, scopes, resource_uri, authorization_expires_at)
VALUES (@subject_urn, @oauth_client_id, @admin_session_id_enc, @scopes, @resource_uri, @authorization_expires_at)
ON CONFLICT (subject_urn, oauth_client_id) WHERE revoked_at IS NULL
DO UPDATE SET admin_session_id_enc = EXCLUDED.admin_session_id_enc,
  scopes = EXCLUDED.scopes,
  resource_uri = EXCLUDED.resource_uri,
  active_generation = generate_uuidv7(),
  authorization_expires_at = EXCLUDED.authorization_expires_at,
  authorized_at = clock_timestamp(),
  reauthorized_at = clock_timestamp(),
  reauthorization_required_at = NULL,
  reauthorization_reason = NULL,
  updated_at = clock_timestamp()
RETURNING id;

-- name: InsertAuthorizationGrant :exec
-- Binds the one-use code to the connection's current generation.
INSERT INTO admin_mcp_authorization_grants
  (authorization_code_hash, oauth_client_id, connection_id, connection_generation,
   redirect_uri, code_challenge, scopes, resource_uri, expires_at)
SELECT @authorization_code_hash::text, @oauth_client_id::uuid, connection.id, connection.active_generation,
  @redirect_uri::text, @code_challenge::text, @scopes::text[], @resource_uri::text, @expires_at::timestamptz
FROM admin_mcp_connections AS connection
WHERE connection.id = @connection_id
  AND connection.revoked_at IS NULL;

-- name: GetAuthorizationGrant :one
SELECT sqlc.embed(connection), auth_grant.code_challenge, auth_grant.redirect_uri
FROM admin_mcp_authorization_grants AS auth_grant
JOIN admin_mcp_connections AS connection
  ON connection.id = auth_grant.connection_id AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.authorization_code_hash = @authorization_code_hash
  AND client.client_id = @client_id
  AND auth_grant.consumed_at IS NULL
  AND auth_grant.revoked_at IS NULL
  AND auth_grant.expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND connection.active_generation = auth_grant.connection_generation
  AND connection.authorization_expires_at > clock_timestamp()
  AND connection.scopes = auth_grant.scopes
  AND connection.resource_uri = auth_grant.resource_uri
  AND client.revoked_at IS NULL
  AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp());

-- name: LockAuthorizationGrant :one
-- Same checks as GetAuthorizationGrant, locking the grant and connection for
-- the one-use code exchange.
SELECT sqlc.embed(connection), auth_grant.code_challenge, auth_grant.redirect_uri
FROM admin_mcp_authorization_grants AS auth_grant
JOIN admin_mcp_connections AS connection
  ON connection.id = auth_grant.connection_id AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.authorization_code_hash = @authorization_code_hash
  AND client.client_id = @client_id
  AND auth_grant.consumed_at IS NULL
  AND auth_grant.revoked_at IS NULL
  AND auth_grant.expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND connection.active_generation = auth_grant.connection_generation
  AND connection.authorization_expires_at > clock_timestamp()
  AND connection.scopes = auth_grant.scopes
  AND connection.resource_uri = auth_grant.resource_uri
  AND client.revoked_at IS NULL
  AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp())
FOR UPDATE OF auth_grant, connection;

-- name: ConsumeAuthorizationGrant :exec
UPDATE admin_mcp_authorization_grants
SET consumed_at = @now,
    updated_at = @now
WHERE authorization_code_hash = @authorization_code_hash;

-- name: InsertStaffSession :exec
INSERT INTO admin_mcp_sessions
  (id, connection_id, oauth_client_id, connection_generation, jti, refresh_token_hash, expires_at, refresh_expires_at)
VALUES (@id, @connection_id, @oauth_client_id, @connection_generation, @jti, @refresh_token_hash, @expires_at, @refresh_expires_at);

-- name: GetRefreshSession :one
SELECT sqlc.embed(connection), session.rotated_at, session.revoked_at, session.refresh_expires_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = @refresh_token_hash
  AND client.client_id = @client_id
  AND client.revoked_at IS NULL
  AND (client.client_secret_expires_at IS NULL OR client.client_secret_expires_at > clock_timestamp())
  AND connection.revoked_at IS NULL
  AND connection.active_generation = session.connection_generation;

-- name: LockReusedRefreshSession :one
-- Loads a presented refresh token regardless of liveness so reuse of a rotated
-- token can terminalize the generation that issued it.
SELECT connection.id AS connection_id,
  session.connection_generation,
  connection.active_generation,
  session.rotated_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection ON connection.id = session.connection_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = @refresh_token_hash
  AND client.client_id = @client_id
FOR UPDATE OF connection, session;

-- name: LockRefreshSession :one
SELECT sqlc.embed(connection),
  session.id AS session_id,
  session.connection_generation AS session_generation,
  session.rotated_at,
  session.revoked_at AS session_revoked_at,
  session.refresh_expires_at,
  client.revoked_at AS client_revoked_at,
  client.client_secret_expires_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = session.oauth_client_id
WHERE session.refresh_token_hash = @refresh_token_hash
  AND client.client_id = @client_id
FOR UPDATE OF session, connection;

-- name: RequireReauthorizationForRefreshReuse :exec
-- Terminalizes only the generation that issued the reused token, not a later
-- staff reauthorization that happened while the request waited.
UPDATE admin_mcp_connections
SET reauthorization_required_at = @now,
    reauthorization_reason = 'refresh_reuse',
    updated_at = @now
WHERE id = @id
  AND active_generation = @generation
  AND revoked_at IS NULL;

-- name: ExtendStaffConnection :exec
-- Restarts the inactivity timeout on refresh. The generation is unchanged, so
-- pending write proposals stay bound to this connection.
UPDATE admin_mcp_connections
SET authorization_expires_at = @authorization_expires_at,
    updated_at = clock_timestamp()
WHERE id = @id
  AND active_generation = @generation
  AND revoked_at IS NULL;

-- name: RotateStaffSession :exec
UPDATE admin_mcp_sessions
SET rotated_at = @now,
    revoked_at = @now,
    replaced_by_session_id = @replaced_by_session_id,
    updated_at = @now
WHERE id = @id;

-- name: GetActiveStaffAccessSession :one
SELECT connection.subject_urn,
  client.client_id,
  client.id::text AS client_row_id,
  connection.id::text AS connection_id,
  session.connection_generation::text AS session_generation,
  connection.active_generation::text AS active_generation,
  connection.resource_uri,
  connection.scopes,
  connection.admin_session_id_enc,
  session.expires_at
FROM admin_mcp_sessions AS session
JOIN admin_mcp_connections AS connection
  ON connection.id = session.connection_id AND connection.oauth_client_id = session.oauth_client_id
JOIN admin_mcp_oauth_clients AS client ON client.id = connection.oauth_client_id
WHERE session.jti = @jti
  AND session.revoked_at IS NULL
  AND session.expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND connection.authorization_expires_at > clock_timestamp()
  AND client.revoked_at IS NULL;

-- name: CountWriteEventsFixture :one
-- Test-only: counts staff event trail rows for one operation, target and event.
SELECT count(*)
FROM admin_mcp_write_events
WHERE operation = @operation
  AND organization_id IS NOT DISTINCT FROM @organization_id
  AND event = @event;

-- name: RevokeOAuthClientFixture :exec
-- Test-only: simulates a staff client revoked during a live connection.
UPDATE admin_mcp_oauth_clients
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: SetConnectionScopesFixture :exec
-- Test-only: simulates a scope change on a live connection.
UPDATE admin_mcp_connections
SET scopes = @scopes,
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: GetConnectionGenerationFixture :one
-- Test-only: reads the connection generation after reconsent.
SELECT active_generation
FROM admin_mcp_connections
WHERE id = @id;

-- name: GetReauthorizationReasonFixture :one
-- Test-only: reads why a connection now requires staff reauthorization.
SELECT reauthorization_reason
FROM admin_mcp_connections
WHERE id = @id;

-- name: RenameOrganizationFixture :execrows
-- Test-only: a synthetic business write run inside a staff write transaction.
UPDATE organization_metadata
SET name = @name
WHERE id = @id;

-- name: RestorePublicCatalogIconAssetFixture :exec
-- Test-only: returns an asset to the public catalog icon shape.
UPDATE assets SET content_type = 'image/svg+xml', project_id = NULL, organization_id = NULL, deleted_at = NULL WHERE id = @id;

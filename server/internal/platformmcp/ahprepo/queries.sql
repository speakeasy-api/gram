-- name: CreateAHPOAuthClient :one
INSERT INTO ahp_oauth_clients (
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

-- name: GetActiveAHPOAuthClientByClientID :one
SELECT *
FROM ahp_oauth_clients
WHERE client_id = @client_id
  AND revoked_at IS NULL;

-- name: UpsertAHPOAuthClientFromCIMD :one
-- Lazy upsert for a client resolved from a Client ID Metadata Document at
-- authorize time. For CIMD rows the document URL IS the client_id, so the
-- conflict target is the same unique index that serves DCR lookups. On
-- refresh the mutable metadata (client_name, redirect_uris) and every cache
-- column are replaced wholesale, including the ETag, which is set to NULL
-- when the response carried no usable validator so the next refresh is
-- unconditional rather than replaying a stale one.
--
-- The cache expiry is derived from the database clock rather than the
-- application's, so it can never land before the client_id_metadata_fetched_at
-- written in the same statement.
--
-- The DO UPDATE is guarded so it can never touch a secret-bearing DCR row
-- that happens to share the client_id, nor resurrect a revoked one:
-- rewriting the former would trip the client_id_metadata_uri CHECK
-- constraints with an opaque 500, and the latter would undo an operator's
-- revocation. Either collision surfaces as no-rows, which the resolver maps
-- to invalid_client.
INSERT INTO ahp_oauth_clients (
    client_id,
    client_secret_hash,
    client_name,
    redirect_uris,
    client_secret_expires_at,
    client_id_metadata_uri,
    client_id_metadata_fetched_at,
    client_id_metadata_cache_expires_at,
    client_id_metadata_etag
) VALUES (
    @client_id,
    NULL,
    @client_name,
    @redirect_uris,
    NULL,
    @client_id,
    clock_timestamp(),
    clock_timestamp() + make_interval(secs => @cache_ttl_seconds::double precision),
    sqlc.narg('client_id_metadata_etag')
)
ON CONFLICT (client_id)
DO UPDATE SET
    client_name = EXCLUDED.client_name,
    redirect_uris = EXCLUDED.redirect_uris,
    client_id_metadata_uri = EXCLUDED.client_id_metadata_uri,
    client_id_metadata_fetched_at = EXCLUDED.client_id_metadata_fetched_at,
    client_id_metadata_cache_expires_at = EXCLUDED.client_id_metadata_cache_expires_at,
    client_id_metadata_etag = EXCLUDED.client_id_metadata_etag,
    updated_at = clock_timestamp()
WHERE ahp_oauth_clients.client_secret_hash IS NULL
  AND ahp_oauth_clients.revoked_at IS NULL
RETURNING *;

-- name: UpdateAHPOAuthClientCIMDCache :one
-- Refreshes the cache bookkeeping on a CIMD-resolved client whose document
-- host answered 304 Not Modified. The stored client_name and redirect_uris
-- are current by definition of the 304, so they are deliberately untouched;
-- only the fetch stamp, the expiry, and the validator move.
--
-- The guards mirror UpsertAHPOAuthClientFromCIMD's, so this statement
-- can never push a row into violating the client_id_metadata_uri CHECK
-- constraints; such a collision surfaces as no-rows, which the resolver maps
-- to invalid_client.
UPDATE ahp_oauth_clients
SET client_id_metadata_fetched_at = clock_timestamp(),
    client_id_metadata_cache_expires_at = clock_timestamp() + make_interval(secs => @cache_ttl_seconds::double precision),
    client_id_metadata_etag = sqlc.narg('client_id_metadata_etag'),
    updated_at = clock_timestamp()
WHERE client_id = @client_id
  AND client_id_metadata_uri IS NOT NULL
  AND client_secret_hash IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: GetAHPOAuthClientForUpdate :one
SELECT *
FROM ahp_oauth_clients
WHERE client_id = @client_id
FOR UPDATE;

-- name: ListAHPClientConnectionsForUpdate :many
SELECT *
FROM ahp_connections
WHERE oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL
FOR UPDATE;

-- name: RevokeAHPOAuthClient :one
UPDATE ahp_oauth_clients
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE client_id = @client_id
  AND revoked_at IS NULL
RETURNING id;

-- name: LockAHPConnectionAuthorization :exec
SELECT pg_advisory_xact_lock(
    hashtextextended(
        format('%s:%s:%s:%s:%s', @organization_id::text, @subject_urn::text, @oauth_client_id::text, @project_id::uuid, @resource_uri::text),
        0
    )
);

-- name: CreateAHPConnection :one
INSERT INTO ahp_connections (
    id,
    organization_id,
    subject_urn,
    oauth_client_id,
    active_generation,
    authorization_expires_at,
    project_id,
    resource_uri
) VALUES (
    @id,
    @organization_id,
    @subject_urn,
    @oauth_client_id,
    @active_generation,
    @authorization_expires_at,
    @project_id,
    @resource_uri
)
RETURNING *;

-- name: GetActiveAHPConnection :one
SELECT *
FROM ahp_connections
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND resource_uri = @resource_uri
  AND subject_urn = @subject_urn
  AND oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL;

-- name: GetActiveAHPConnectionByID :one
SELECT connection.*, client.client_id
FROM ahp_connections AS connection
JOIN ahp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
WHERE connection.id = @id
  AND connection.organization_id = @organization_id
  AND connection.revoked_at IS NULL
  AND client.revoked_at IS NULL;

-- name: GetAHPConnectionForUpdate :one
SELECT connection.*, client.client_id, client.revoked_at AS client_revoked_at
FROM ahp_connections AS connection
JOIN ahp_oauth_clients AS client
  ON client.id = connection.oauth_client_id
WHERE connection.id = @id
  AND connection.organization_id = @organization_id
FOR UPDATE OF connection;

-- name: RevokeAHPConnection :one
UPDATE ahp_connections
SET revoked_at = @revoked_at,
    reauthorization_required_at = @revoked_at,
    reauthorization_reason = 'connection_revoked',
    updated_at = @revoked_at
WHERE id = @id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: MarkAHPConnectionReauthorizationRequired :one
UPDATE ahp_connections
SET reauthorization_required_at = @reauthorization_required_at,
    reauthorization_reason = @reauthorization_reason,
    updated_at = @reauthorization_required_at
WHERE id = @connection_id
  AND organization_id = @organization_id
  AND active_generation = @connection_generation
  AND revoked_at IS NULL
RETURNING *;

-- name: RotateAHPConnectionGeneration :one
UPDATE ahp_connections
SET active_generation = @active_generation,
    reauthorized_at = @reauthorized_at,
    authorization_expires_at = @authorization_expires_at,
    reauthorization_required_at = NULL,
    reauthorization_reason = NULL,
    updated_at = @reauthorized_at
WHERE id = @connection_id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: CreateAHPAuthorizationGrant :one
INSERT INTO ahp_authorization_grants (
    project_id,
    organization_id,
    authorization_code_hash,
    oauth_client_id,
    connection_id,
    connection_generation,
    redirect_uri,
    code_challenge,
    expires_at
) VALUES (
    (SELECT project_id FROM ahp_connections WHERE id = @connection_id AND organization_id = @organization_id AND oauth_client_id = @oauth_client_id),
    @organization_id,
    @authorization_code_hash,
    @oauth_client_id,
    @connection_id,
    @connection_generation,
    @redirect_uri,
    @code_challenge,
    @expires_at
)
RETURNING *;

-- name: GetAHPAuthorizationGrantForValidation :one
SELECT
    auth_grant.*,
    connection.resource_uri, connection.subject_urn,
    connection.active_generation,
    client.client_id,
    COALESCE(
        connection.authorization_expires_at,
        COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
    )::timestamptz AS effective_authorization_expires_at
FROM ahp_authorization_grants AS auth_grant
JOIN ahp_connections AS connection
  ON connection.id = auth_grant.connection_id
  AND connection.organization_id = auth_grant.organization_id
  AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN ahp_oauth_clients AS client
  ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.organization_id = @organization_id
  AND auth_grant.authorization_code_hash = @authorization_code_hash
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND client.revoked_at IS NULL;

-- name: GetAHPAuthorizationGrantForConsume :one
SELECT
    auth_grant.*,
    connection.resource_uri, connection.subject_urn,
    connection.active_generation,
    client.client_id,
    COALESCE(
        connection.authorization_expires_at,
        COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
    )::timestamptz AS effective_authorization_expires_at
FROM ahp_authorization_grants AS auth_grant
JOIN ahp_connections AS connection
  ON connection.id = auth_grant.connection_id
  AND connection.organization_id = auth_grant.organization_id
  AND connection.oauth_client_id = auth_grant.oauth_client_id
JOIN ahp_oauth_clients AS client
  ON client.id = auth_grant.oauth_client_id
WHERE auth_grant.organization_id = @organization_id
  AND auth_grant.authorization_code_hash = @authorization_code_hash
  AND connection.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND client.revoked_at IS NULL
FOR UPDATE OF auth_grant, connection;

-- name: ConsumeAHPAuthorizationGrant :one
UPDATE ahp_authorization_grants
SET consumed_at = @consumed_at,
    updated_at = @consumed_at
WHERE id = @id
  AND organization_id = @organization_id
  AND consumed_at IS NULL
  AND revoked_at IS NULL
RETURNING *;

-- name: CreateAHPSession :one
INSERT INTO ahp_sessions (
    id,
    project_id,
    organization_id,
    connection_id,
    oauth_client_id,
    connection_generation,
    jti,
    refresh_token_hash,
    expires_at,
    refresh_expires_at
) VALUES (
    @id,
    (SELECT project_id FROM ahp_connections WHERE id = @connection_id AND organization_id = @organization_id AND oauth_client_id = @oauth_client_id),
    @organization_id,
    @connection_id,
    @oauth_client_id,
    @connection_generation,
    @jti,
    @refresh_token_hash,
    @expires_at,
    @refresh_expires_at
)
RETURNING *;

-- name: GetAHPSessionForRefresh :one
SELECT session.*, connection.resource_uri, connection.subject_urn, connection.active_generation, client.client_id
FROM ahp_sessions AS session
JOIN ahp_connections AS connection
  ON connection.id = session.connection_id
  AND connection.organization_id = session.organization_id
  AND connection.oauth_client_id = session.oauth_client_id
JOIN ahp_oauth_clients AS client
  ON client.id = session.oauth_client_id
WHERE session.organization_id = @organization_id
  AND session.refresh_token_hash = @refresh_token_hash
  AND session.revoked_at IS NULL
  AND session.refresh_expires_at > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.active_generation = session.connection_generation
  AND client.revoked_at IS NULL;

-- name: GetAHPSessionForRefreshForUpdate :one
-- Lock the connection before its session so refresh, connection revocation, and
-- client revocation all use the same connection -> session lock order.
WITH target_session AS MATERIALIZED (
    SELECT session.connection_id, session.oauth_client_id
    FROM ahp_sessions AS session
    WHERE session.organization_id = @organization_id
      AND session.refresh_token_hash = @refresh_token_hash
),
locked_connection AS MATERIALIZED (
    SELECT connection.*
    FROM ahp_connections AS connection
    JOIN target_session
      ON target_session.connection_id = connection.id
      AND target_session.oauth_client_id = connection.oauth_client_id
    WHERE connection.organization_id = @organization_id
    FOR UPDATE OF connection
),
locked_session AS MATERIALIZED (
    SELECT session.*
    FROM ahp_sessions AS session
    JOIN locked_connection AS connection
      ON connection.id = session.connection_id
      AND connection.organization_id = session.organization_id
      AND connection.oauth_client_id = session.oauth_client_id
    WHERE session.organization_id = @organization_id
      AND session.refresh_token_hash = @refresh_token_hash
    FOR UPDATE OF session
)
SELECT
    session.*,
    connection.resource_uri, connection.subject_urn,
    connection.active_generation,
    connection.revoked_at AS connection_revoked_at,
    connection.reauthorization_required_at,
    connection.reauthorization_reason,
    client.client_id,
    client.revoked_at AS client_revoked_at,
    COALESCE(
        connection.authorization_expires_at,
        COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
    )::timestamptz AS effective_authorization_expires_at
FROM locked_session AS session
JOIN locked_connection AS connection
  ON connection.id = session.connection_id
  AND connection.organization_id = session.organization_id
  AND connection.oauth_client_id = session.oauth_client_id
JOIN ahp_oauth_clients AS client
  ON client.id = session.oauth_client_id;

-- name: RotateAHPSession :one
UPDATE ahp_sessions
SET revoked_at = @rotated_at,
    rotated_at = @rotated_at,
    replaced_by_session_id = @replaced_by_session_id,
    updated_at = @rotated_at
WHERE id = @id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAHPSession :one
UPDATE ahp_sessions
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE id = @id
  AND organization_id = @organization_id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAHPSessionByJTI :one
UPDATE ahp_sessions
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE organization_id = @organization_id
  AND jti = @jti
  AND oauth_client_id = @oauth_client_id
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAHPSessionFamily :exec
UPDATE ahp_sessions
SET revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE organization_id = @organization_id
  AND connection_id = @connection_id
  AND connection_generation = @connection_generation
  AND revoked_at IS NULL;

-- name: GetActiveAHPSessionByJTI :one
SELECT
    session.*,
    connection.resource_uri, connection.subject_urn,
    connection.active_generation,
    client.client_id
FROM ahp_sessions AS session
JOIN ahp_connections AS connection
  ON connection.id = session.connection_id
  AND connection.organization_id = session.organization_id
  AND connection.oauth_client_id = session.oauth_client_id
JOIN ahp_oauth_clients AS client
  ON client.id = session.oauth_client_id
WHERE session.organization_id = @organization_id
  AND session.jti = @jti
  AND EXISTS (SELECT 1 FROM projects p WHERE p.id = session.project_id AND p.organization_id = session.organization_id AND p.deleted IS FALSE)
  AND session.expires_at > clock_timestamp()
  AND session.revoked_at IS NULL
  AND connection.reauthorization_required_at IS NULL
  AND COALESCE(
      connection.authorization_expires_at,
      COALESCE(connection.reauthorized_at, connection.authorized_at) + INTERVAL '90 days'
  ) > clock_timestamp()
  AND connection.revoked_at IS NULL
  AND connection.active_generation = session.connection_generation
  AND client.revoked_at IS NULL;


-- name: FixtureRebindAHPGrantProject :exec
UPDATE ahp_authorization_grants
SET project_id = @replacement_project_id
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND connection_id = @connection_id;

-- name: FixtureRebindAHPSessionProject :exec
UPDATE ahp_sessions
SET project_id = @replacement_project_id
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND connection_id = @connection_id;

-- name: FixtureSetAHPAuthorizationTimes :exec
UPDATE ahp_connections
SET authorization_expires_at = NULL,
    authorized_at = @authorized_at,
    reauthorized_at = sqlc.narg('reauthorized_at')
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @connection_id;

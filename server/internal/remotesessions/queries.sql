-- Remote session issuers — upstream Authorization Server identity records
-- that Gram talks to as an OAuth client.

-- name: CreateRemoteSessionIssuer :one
INSERT INTO remote_session_issuers (
    project_id,
    slug,
    issuer,
    authorization_endpoint,
    token_endpoint,
    registration_endpoint,
    jwks_uri,
    scopes_supported,
    grant_types_supported,
    response_types_supported,
    token_endpoint_auth_methods_supported,
    oidc,
    passthrough
)
VALUES (
    @project_id,
    @slug,
    @issuer,
    @authorization_endpoint,
    @token_endpoint,
    @registration_endpoint,
    @jwks_uri,
    @scopes_supported,
    @grant_types_supported,
    @response_types_supported,
    @token_endpoint_auth_methods_supported,
    @oidc,
    @passthrough
)
RETURNING *;

-- name: GetRemoteSessionIssuerByID :one
SELECT *
FROM remote_session_issuers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetRemoteSessionIssuerBySlug :one
SELECT *
FROM remote_session_issuers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListRemoteSessionIssuersByProjectID :many
SELECT *
FROM remote_session_issuers
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC
LIMIT 100;

-- name: UpdateRemoteSessionIssuer :one
UPDATE remote_session_issuers
SET
    slug = COALESCE(sqlc.narg('slug'), slug),
    issuer = COALESCE(sqlc.narg('issuer'), issuer),
    authorization_endpoint = COALESCE(sqlc.narg('authorization_endpoint'), authorization_endpoint),
    token_endpoint = COALESCE(sqlc.narg('token_endpoint'), token_endpoint),
    registration_endpoint = COALESCE(sqlc.narg('registration_endpoint'), registration_endpoint),
    jwks_uri = COALESCE(sqlc.narg('jwks_uri'), jwks_uri),
    scopes_supported = COALESCE(sqlc.narg('scopes_supported')::text[], scopes_supported),
    grant_types_supported = COALESCE(sqlc.narg('grant_types_supported')::text[], grant_types_supported),
    response_types_supported = COALESCE(sqlc.narg('response_types_supported')::text[], response_types_supported),
    token_endpoint_auth_methods_supported = COALESCE(sqlc.narg('token_endpoint_auth_methods_supported')::text[], token_endpoint_auth_methods_supported),
    oidc = COALESCE(sqlc.narg('oidc'), oidc),
    passthrough = COALESCE(sqlc.narg('passthrough'), passthrough),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteRemoteSessionIssuer :one
UPDATE remote_session_issuers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: CountRemoteSessionClientsByIssuerID :one
SELECT COUNT(*)
FROM remote_session_clients
WHERE remote_session_issuer_id = @remote_session_issuer_id AND deleted IS FALSE;

-- Remote session clients — credentials Gram uses when acting as an OAuth
-- client of a remote_session_issuer. client_secret_encrypted is stored
-- encrypted via the project encryption key.

-- name: CreateRemoteSessionClient :one
INSERT INTO remote_session_clients (
    project_id,
    remote_session_issuer_id,
    user_session_issuer_id,
    client_id,
    client_secret_encrypted,
    client_id_issued_at,
    client_secret_expires_at
)
VALUES (
    @project_id,
    @remote_session_issuer_id,
    @user_session_issuer_id,
    @client_id,
    @client_secret_encrypted,
    @client_id_issued_at,
    @client_secret_expires_at
)
RETURNING *;

-- name: GetRemoteSessionClientByID :one
SELECT *
FROM remote_session_clients
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListRemoteSessionClientsByProjectID :many
SELECT *
FROM remote_session_clients
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('remote_session_issuer_id')::uuid IS NULL OR remote_session_issuer_id = sqlc.narg('remote_session_issuer_id')::uuid)
  AND (sqlc.narg('user_session_issuer_id')::uuid IS NULL OR user_session_issuer_id = sqlc.narg('user_session_issuer_id')::uuid)
ORDER BY created_at DESC
LIMIT 100;

-- name: UpdateRemoteSessionClient :one
UPDATE remote_session_clients
SET
    client_secret_encrypted = COALESCE(sqlc.narg('client_secret_encrypted'), client_secret_encrypted),
    client_secret_expires_at = COALESCE(sqlc.narg('client_secret_expires_at'), client_secret_expires_at),
    user_session_issuer_id = COALESCE(sqlc.narg('user_session_issuer_id'), user_session_issuer_id),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteRemoteSessionClient :one
UPDATE remote_session_clients
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteRemoteSessionsByClientID :execrows
UPDATE remote_sessions
SET deleted_at = clock_timestamp()
WHERE remote_session_client_id = @remote_session_client_id AND deleted IS FALSE;

-- name: CountActiveRemoteSessionsByClientID :one
SELECT COUNT(*)
FROM remote_sessions
WHERE remote_session_client_id = @remote_session_client_id AND deleted IS FALSE;

-- name: InsertRemoteSession :one
INSERT INTO remote_sessions (
    principal_urn,
    user_session_issuer_id,
    remote_session_client_id,
    access_token_encrypted,
    access_expires_at
)
VALUES (
    @principal_urn,
    @user_session_issuer_id,
    @remote_session_client_id,
    @access_token_encrypted,
    @access_expires_at
)
RETURNING *;

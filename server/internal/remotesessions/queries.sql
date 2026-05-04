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
ORDER BY created_at DESC;

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

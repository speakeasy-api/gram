-- External OAuth Server Metadata Queries

-- name: CreateExternalOAuthServerMetadata :one
INSERT INTO external_oauth_server_metadata (
    project_id,
    slug,
    metadata
) VALUES (
    @project_id,
    @slug,
    @metadata
) RETURNING *;

-- name: GetExternalOAuthServerMetadata :one
SELECT * FROM external_oauth_server_metadata
WHERE project_id = @project_id AND id = @id AND deleted IS FALSE;

-- name: DeleteExternalOAuthServerMetadata :exec
UPDATE external_oauth_server_metadata SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id AND id = @id;

-- OAuth Proxy Servers Queries

-- name: UpsertOAuthProxyServer :one
INSERT INTO oauth_proxy_servers (
    project_id,
    slug
) VALUES (
    @project_id,
    @slug
) ON CONFLICT (project_id, slug) WHERE deleted IS FALSE DO UPDATE SET
    updated_at = clock_timestamp()
RETURNING *;

-- name: GetOAuthProxyServer :one
SELECT *
FROM oauth_proxy_servers s
WHERE s.project_id = @project_id AND s.id = @id AND s.deleted IS FALSE;

-- name: DeleteOAuthProxyServer :exec
UPDATE oauth_proxy_servers SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id AND id = @id;

-- OAuth Proxy Providers Queries

-- name: UpsertOAuthProxyProvider :one
INSERT INTO oauth_proxy_providers (
    project_id,
    oauth_proxy_server_id,
    slug,
    provider_type,
    authorization_endpoint,
    token_endpoint,
    registration_endpoint,
    scopes_supported,
    response_types_supported,
    response_modes_supported,
    grant_types_supported,
    token_endpoint_auth_methods_supported,
    security_key_names,
    secrets
) VALUES (
    @project_id,
    @oauth_proxy_server_id,
    @slug,
    @provider_type,
    @authorization_endpoint,
    @token_endpoint,
    @registration_endpoint,
    @scopes_supported,
    @response_types_supported,
    @response_modes_supported,
    @grant_types_supported,
    @token_endpoint_auth_methods_supported,
    @security_key_names,
    @secrets
) ON CONFLICT (project_id, slug) WHERE deleted IS FALSE DO UPDATE SET
    oauth_proxy_server_id = EXCLUDED.oauth_proxy_server_id,
    provider_type = EXCLUDED.provider_type,
    authorization_endpoint = EXCLUDED.authorization_endpoint,
    token_endpoint = EXCLUDED.token_endpoint,
    registration_endpoint = EXCLUDED.registration_endpoint,
    scopes_supported = EXCLUDED.scopes_supported,
    response_types_supported = EXCLUDED.response_types_supported,
    response_modes_supported = EXCLUDED.response_modes_supported,
    grant_types_supported = EXCLUDED.grant_types_supported,
    token_endpoint_auth_methods_supported = EXCLUDED.token_endpoint_auth_methods_supported,
    security_key_names = EXCLUDED.security_key_names,
    secrets = EXCLUDED.secrets,
    updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteOAuthProxyProvider :exec
UPDATE oauth_proxy_providers SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id AND id = @id;

-- name: ListOAuthProxyProvidersByServer :many
SELECT * FROM oauth_proxy_providers
WHERE oauth_proxy_server_id = @oauth_proxy_server_id AND project_id = @project_id AND deleted IS FALSE
ORDER BY created_at ASC;

-- User OAuth Tokens Queries
-- Stores tokens obtained from external OAuth providers for users authenticating to external MCP servers

-- name: UpsertUserOAuthToken :one
INSERT INTO user_oauth_tokens (
    user_id,
    organization_id,
    oauth_server_issuer,
    access_token_encrypted,
    refresh_token_encrypted,
    token_type,
    expires_at,
    scopes,
    provider_name
) VALUES (
    @user_id,
    @organization_id,
    @oauth_server_issuer,
    @access_token_encrypted,
    @refresh_token_encrypted,
    @token_type,
    @expires_at,
    @scopes,
    @provider_name
) ON CONFLICT (user_id, organization_id, oauth_server_issuer) WHERE deleted IS FALSE DO UPDATE SET
    access_token_encrypted = EXCLUDED.access_token_encrypted,
    refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
    token_type = EXCLUDED.token_type,
    expires_at = EXCLUDED.expires_at,
    scopes = EXCLUDED.scopes,
    provider_name = EXCLUDED.provider_name,
    updated_at = clock_timestamp()
RETURNING *;

-- name: GetUserOAuthToken :one
SELECT * FROM user_oauth_tokens
WHERE user_id = @user_id
  AND organization_id = @organization_id
  AND oauth_server_issuer = @oauth_server_issuer
  AND deleted IS FALSE;

-- name: GetUserOAuthTokenByID :one
SELECT * FROM user_oauth_tokens
WHERE id = @id AND deleted IS FALSE;

-- name: ListUserOAuthTokens :many
SELECT * FROM user_oauth_tokens
WHERE user_id = @user_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: DeleteUserOAuthToken :exec
UPDATE user_oauth_tokens SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: DeleteUserOAuthTokenByIssuer :exec
UPDATE user_oauth_tokens SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE user_id = @user_id
  AND organization_id = @organization_id
  AND oauth_server_issuer = @oauth_server_issuer;

-- External OAuth Client Registrations Queries
-- Stores client credentials from Dynamic Client Registration (DCR)
-- These are organization-level credentials, not user-level

-- name: UpsertExternalOAuthClientRegistration :one
INSERT INTO external_oauth_client_registrations (
    organization_id,
    oauth_server_issuer,
    client_id,
    client_secret_encrypted,
    client_id_issued_at,
    client_secret_expires_at
) VALUES (
    @organization_id,
    @oauth_server_issuer,
    @client_id,
    @client_secret_encrypted,
    @client_id_issued_at,
    @client_secret_expires_at
) ON CONFLICT (organization_id, oauth_server_issuer) WHERE deleted IS FALSE DO UPDATE SET
    client_id = EXCLUDED.client_id,
    client_secret_encrypted = EXCLUDED.client_secret_encrypted,
    client_id_issued_at = EXCLUDED.client_id_issued_at,
    client_secret_expires_at = EXCLUDED.client_secret_expires_at,
    updated_at = clock_timestamp()
RETURNING *;

-- name: GetExternalOAuthClientRegistration :one
SELECT * FROM external_oauth_client_registrations
WHERE organization_id = @organization_id
  AND oauth_server_issuer = @oauth_server_issuer
  AND deleted IS FALSE;

-- name: DeleteExternalOAuthClientRegistration :exec
UPDATE external_oauth_client_registrations SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND oauth_server_issuer = @oauth_server_issuer;

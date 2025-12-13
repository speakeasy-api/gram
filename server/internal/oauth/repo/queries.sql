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

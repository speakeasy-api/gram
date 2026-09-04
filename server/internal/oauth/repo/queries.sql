-- External OAuth Server Metadata Queries

-- name: CreateExternalOAuthServerMetadata :one
INSERT INTO external_oauth_server_metadata (
    project_id,
    slug,
    metadata,
    authorization_server_issuer
) VALUES (
    @project_id,
    @slug,
    @metadata,
    @authorization_server_issuer
) RETURNING *;

-- name: CreateGeneratedExternalOAuthServerMetadata :one
INSERT INTO external_oauth_server_metadata (
    project_id,
    slug,
    metadata,
    authorization_server_issuer
) VALUES (
    @project_id,
    @slug,
    @metadata,
    @authorization_server_issuer
)
ON CONFLICT (project_id, slug) WHERE deleted IS FALSE DO NOTHING
RETURNING *;

-- name: UpdateExternalOAuthServerSource :one
UPDATE external_oauth_server_metadata
SET metadata = @metadata,
    authorization_server_issuer = @authorization_server_issuer,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: GetExternalOAuthServerMetadata :one
SELECT * FROM external_oauth_server_metadata
WHERE project_id = @project_id AND id = @id AND deleted IS FALSE;

-- name: DeleteExternalOAuthServerMetadata :one
UPDATE external_oauth_server_metadata SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id AND id = @id
RETURNING id, slug;

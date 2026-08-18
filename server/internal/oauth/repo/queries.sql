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

-- name: DeleteExternalOAuthServerMetadata :one
UPDATE external_oauth_server_metadata SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id AND id = @id
RETURNING id, slug;

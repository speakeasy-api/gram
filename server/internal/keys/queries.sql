-- name: CreateAPIKey :one
INSERT INTO api_keys (
    organization_id
  , project_id
  , created_by_user_id
  , name
  , token
  , scopes
) VALUES (
    @organization_id
  , @project_id
  , @created_by_user_id
  , @name
  , @token
  , @scopes::text[]
)
RETURNING *;

-- name: GetAPIKeyByToken :one
SELECT *
FROM api_keys
WHERE token = @token
  AND deleted IS FALSE;

-- name: ListAPIKeysByOrganization :many
SELECT *
FROM api_keys
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: DeleteAPIKey :exec
UPDATE api_keys
SET deleted_at = NOW()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

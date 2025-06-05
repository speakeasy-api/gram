-- name: CreateAPIKey :one
INSERT INTO api_keys (
    organization_id
  , project_id
  , created_by_user_id
  , name
  , key_prefix
  , key_hash
  , scopes
) VALUES (
    @organization_id
  , @project_id
  , @created_by_user_id
  , @name
  , @key_prefix
  , @key_hash
  , @scopes::text[]
)
RETURNING *;

-- name: GetAPIKeyByKeyHash :one
SELECT *
FROM api_keys
WHERE key_hash = @key_hash
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

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
RETURNING 
    id
  , organization_id
  , project_id
  , created_by_user_id
  , name
  , token
  , scopes
  , created_at
  , updated_at
  , deleted_at
  , deleted;

-- name: GetAPIKeyByToken :one
SELECT 
    id
  , organization_id
  , project_id
  , created_by_user_id
  , name
  , token
  , scopes
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM api_keys
WHERE token = @token
  AND deleted_at IS NULL;

-- name: ListAPIKeysByOrganization :many
SELECT 
    id
  , organization_id
  , project_id
  , created_by_user_id
  , name
  , token
  , scopes
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM api_keys
WHERE organization_id = @organization_id
  AND deleted_at IS NULL
ORDER BY created_at DESC;

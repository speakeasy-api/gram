-- name: CreateGramKey :one
INSERT INTO gram_keys (
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

-- name: GetGramKeyByToken :one
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
FROM gram_keys
WHERE token = @token
  AND deleted_at IS NULL;

-- name: ListGramKeysByOrganization :many
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
FROM gram_keys
WHERE organization_id = @organization_id
  AND deleted_at IS NULL
ORDER BY created_at DESC;

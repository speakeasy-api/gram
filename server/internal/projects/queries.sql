-- name: GetProject :one
SELECT 
    id
  , name
  , slug
  , organization_id
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM projects
WHERE id = @id;

-- name: CreateProject :one
INSERT INTO projects (
    name
  , slug
  , organization_id
) VALUES (
    @name
  , @slug
  , @organization_id
)
RETURNING 
    id
  , name
  , slug
  , organization_id
  , created_at
  , updated_at
  , deleted_at
  , deleted;

-- name: ListProjectsByOrganization :many
SELECT 
    id
  , name
  , slug
  , organization_id
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM projects
WHERE organization_id = @organization_id
  AND deleted_at IS NULL
ORDER BY created_at DESC;

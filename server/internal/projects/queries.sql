-- name: GetProject :one
SELECT 
    id
  , organization_id
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM projects
WHERE id = @id;

-- name: CreateProject :one
INSERT INTO projects (
    organization_id
) VALUES (
    @organization_id
)
RETURNING 
    id
  , organization_id
  , created_at
  , updated_at
  , deleted_at
  , deleted;

-- name: ListProjectsByOrganization :many
SELECT 
    id
  , organization_id
  , created_at
  , updated_at
  , deleted_at
  , deleted
FROM projects
WHERE organization_id = @organization_id
  AND deleted_at IS NULL
ORDER BY created_at DESC;

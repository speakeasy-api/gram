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
  AND deleted IS FALSE
ORDER BY created_at DESC;
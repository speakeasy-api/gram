-- name: CanAccessProject :one
SELECT id, deleted
FROM projects
WHERE
  organization_id = @organization_id
  AND slug = @project_slug
  AND deleted_at IS NULL
LIMIT 1;
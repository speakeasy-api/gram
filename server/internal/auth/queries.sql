-- name: ListProjectsByOrganization :many
SELECT *
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: PokeProjectByID :one
SELECT id
FROM projects
WHERE
  organization_id = @organization_id
  AND id = @project_id
  AND deleted IS FALSE;
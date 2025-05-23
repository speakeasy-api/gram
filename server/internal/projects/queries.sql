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
RETURNING *;

-- name: ListProjectsByOrganization :many
SELECT *
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY id DESC;

-- name: GetProjectByID :one
SELECT *
FROM projects
WHERE id = @id
  AND deleted IS FALSE;

-- name: SetUserAdminFixture :exec
UPDATE users
SET admin = @admin
WHERE id = @user_id;

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
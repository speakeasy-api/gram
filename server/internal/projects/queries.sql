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
ORDER BY id ASC;

-- name: GetProjectByID :one
SELECT *
FROM projects
WHERE id = @id
  AND deleted IS FALSE;

-- name: GetProjectWithOrganizationMetadata :one
SELECT 
    -- Project fields
    p.id as project_id,
    p.name as project_name,
    p.slug as project_slug,
    
    -- Organization metadata fields
    om.*
    
FROM projects p
INNER JOIN organization_metadata om ON p.organization_id = om.id
WHERE p.deleted IS FALSE
  AND p.id = @id;
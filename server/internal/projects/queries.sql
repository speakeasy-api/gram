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

-- name: UploadProjectLogo :one
UPDATE projects
SET logo_asset_id = @logo_asset_id,
    updated_at = clock_timestamp()
WHERE id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: ListAllowedOriginsByProjectID :many
SELECT *
FROM project_allowed_origins
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListApprovedOriginsByProjectID :many
SELECT origin
FROM project_allowed_origins
WHERE project_id = @project_id
  AND status = 'approved'
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpsertAllowedOrigin :one
INSERT INTO project_allowed_origins (
    project_id
  , origin
  , status
) VALUES (
    @project_id
  , @origin
  , @status
)
ON CONFLICT (project_id, origin) WHERE deleted IS FALSE
DO UPDATE SET
    status = EXCLUDED.status,
    updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteProject :exec
UPDATE projects
SET deleted_at = clock_timestamp()
WHERE id = @id;

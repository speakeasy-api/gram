-- name: CreateEnvironment :one
INSERT INTO environments (
    organization_id,
    project_id,
    name,
    slug,
    description
) VALUES (
    @organization_id,
    @project_id,
    @name,
    @slug,
    @description
) RETURNING id, organization_id, project_id, name, slug, description, created_at, updated_at, deleted;

-- name: ListEnvironments :many
SELECT 
    e.id,
    e.organization_id,
    e.project_id,
    e.name,
    e.slug,
    e.description,
    e.created_at,
    e.updated_at,
    e.deleted
FROM environments e
WHERE e.project_id = $1 AND e.deleted IS FALSE
ORDER BY e.created_at DESC;

-- name: GetEnvironment :one
SELECT 
    e.id,
    e.organization_id,
    e.project_id,
    e.name,
    e.slug,
    e.description,
    e.created_at,
    e.updated_at,
    e.deleted
FROM environments e
WHERE e.slug = $1 AND e.project_id = $2 AND e.deleted IS FALSE;


-- name: UpdateEnvironment :one
UPDATE environments
SET 
    name = COALESCE(@name, name),
    description = COALESCE(@description, description),
    updated_at = now()
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE
RETURNING id, organization_id, project_id, name, slug, description, updated_at, deleted;

-- name: ListEnvironmentEntries :many
SELECT 
    ee.name as name,
    ee.value as value,
    ee.created_at as created_at,
    ee.updated_at as updated_at
FROM environment_entries ee
WHERE ee.environment_id = $1
ORDER BY ee.name ASC;

-- name: DeleteEnvironment :exec
UPDATE environments
SET deleted_at = now()
WHERE slug = $1 AND project_id = $2 AND deleted IS FALSE;

-- name: CreateEnvironmentEntries :many
INSERT INTO environment_entries (
    environment_id,
    name,
    value
) 
/*
 Parameters:
 - environment_id: uuid
 - names: text[]
 - values: text[]
*/
VALUES (
    @environment_id::uuid,
    unnest(@names::text[]),
    unnest(@values::text[])
)
RETURNING *;

-- name: UpsertEnvironmentEntry :one
INSERT INTO environment_entries (environment_id, name, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (environment_id, name) 
DO UPDATE SET 
    value = EXCLUDED.value,
    updated_at = now()
RETURNING *;

-- name: DeleteEnvironmentEntry :exec
DELETE FROM environment_entries
WHERE environment_id = $1 AND name = $2;

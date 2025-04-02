-- name: CreateEnvironment :one
INSERT INTO environments (
    organization_id,
    project_id,
    name,
    slug
) VALUES (
    $1,
    $2,
    $3,
    $4
) RETURNING *;

-- name: ListEnvironments :many
SELECT e.*
FROM environments e
WHERE e.project_id = $1 AND e.deleted_at IS NULL
ORDER BY e.created_at DESC;

-- name: GetEnvironmentBySlug :one
SELECT e.*
FROM environments e
WHERE e.slug = $1 AND e.project_id = $2 AND e.deleted_at IS NULL;

-- name: GetEnvironment :one
SELECT e.*
FROM environments e
WHERE e.id = $1 AND e.deleted_at IS NULL;

-- name: ListEnvironmentEntries :many
SELECT 
    ee.name as name,
    ee.value as value,
    ee.created_at as created_at,
    ee.updated_at as updated_at,
    ee.deleted_at as deleted_at
FROM environment_entries ee
WHERE ee.environment_id = $1 AND ee.deleted_at IS NULL
ORDER BY ee.name ASC;

-- name: DeleteEnvironment :exec
UPDATE environments
SET deleted_at = now()
WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL;

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

-- name: UpdateEnvironmentEntry :one
UPDATE environment_entries
SET 
    value = $3,
    updated_at = now()
WHERE environment_id = $1 AND name = $2 AND deleted_at IS NULL
RETURNING *;

-- name: DeleteEnvironmentEntry :exec
UPDATE environment_entries
SET deleted_at = now()
WHERE environment_id = $1 AND name = $2 AND deleted_at IS NULL;

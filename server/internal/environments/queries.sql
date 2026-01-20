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
) RETURNING *;

-- name: ListEnvironments :many
SELECT *
FROM environments e
WHERE e.project_id = $1 AND e.deleted IS FALSE
ORDER BY e.created_at DESC;

-- name: GetEnvironmentBySlug :one
-- returns: GetEnvironmentByIDRow
SELECT *
FROM environments e
WHERE e.slug = $1 AND e.project_id = $2 AND e.deleted IS FALSE;

-- name: GetEnvironmentByID :one
SELECT *
FROM environments e
WHERE e.id = $1 AND e.project_id = $2 AND e.deleted IS FALSE;


-- name: UpdateEnvironment :one
UPDATE environments
SET
    name = COALESCE(@name, name),
    description = COALESCE(@description, description),
    updated_at = now()
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: ListEnvironmentEntries :many
SELECT ee.*
FROM environment_entries ee
INNER JOIN environments e ON ee.environment_id = e.id
WHERE
    e.project_id = @project_id AND
    ee.environment_id = @environment_id
ORDER BY ee.name ASC;

-- name: DeleteEnvironment :exec
WITH deleted_env AS (
    UPDATE environments
    SET deleted_at = now()
    WHERE environments.slug = $1 AND environments.project_id = $2 AND environments.deleted IS FALSE
    RETURNING slug, project_id
)
UPDATE toolsets
SET default_environment_slug = NULL
FROM deleted_env
WHERE toolsets.default_environment_slug = deleted_env.slug AND toolsets.project_id = deleted_env.project_id;

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

-- name: GetEnvironmentForSource :one
SELECT e.*
FROM environments e
INNER JOIN source_environments se ON se.environment_id = e.id
WHERE se.source_kind = @source_kind
    AND se.source_slug = @source_slug
    AND se.project_id = @project_id
    AND e.deleted IS FALSE;

-- name: SetSourceEnvironment :one
INSERT INTO source_environments (
    source_kind,
    source_slug,
    project_id,
    environment_id
) VALUES (
    @source_kind,
    @source_slug,
    @project_id,
    @environment_id
)
ON CONFLICT (source_kind, source_slug, project_id)
DO UPDATE SET
    environment_id = EXCLUDED.environment_id,
    updated_at = now()
RETURNING *;

-- name: DeleteSourceEnvironment :exec
DELETE FROM source_environments
WHERE source_kind = @source_kind AND source_slug = @source_slug AND project_id = @project_id;

-- name: GetEnvironmentForToolset :one
SELECT e.*
FROM environments e
INNER JOIN toolset_environments te ON te.environment_id = e.id
WHERE te.toolset_id = @toolset_id
    AND te.project_id = @project_id
    AND e.deleted IS FALSE;

-- name: SetToolsetEnvironment :one
INSERT INTO toolset_environments (
    toolset_id,
    project_id,
    environment_id
) VALUES (
    @toolset_id,
    @project_id,
    @environment_id
)
ON CONFLICT (toolset_id)
DO UPDATE SET
    environment_id = EXCLUDED.environment_id,
    updated_at = now()
RETURNING *;

-- name: DeleteToolsetEnvironment :exec
DELETE FROM toolset_environments
WHERE toolset_id = @toolset_id AND project_id = @project_id;

-- name: UpdateEntryDisplayNames :one
-- Merges the provided display names into the existing JSONB map.
UPDATE environments
SET entry_display_names = COALESCE(entry_display_names || @entry_display_names::JSONB, entry_display_names),
    updated_at = clock_timestamp()
WHERE id = @environment_id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: RemoveEntryDisplayNames :one
-- Removes the specified keys from the entry_display_names JSONB map.
UPDATE environments
SET entry_display_names = entry_display_names - @entry_names::TEXT[],
    updated_at = clock_timestamp()
WHERE id = @environment_id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

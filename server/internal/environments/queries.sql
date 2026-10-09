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

-- name: ListEnvironmentEntriesForUpdate :many
-- Same as ListEnvironmentEntries, but holds a row lock on every entry until the
-- transaction ends. An update that omits a value preserves the stored one by
-- reading it and writing it back, so a concurrent rotation landing between the
-- read and the write would otherwise be reverted.
SELECT ee.*
FROM environment_entries ee
INNER JOIN environments e ON ee.environment_id = e.id
WHERE
    e.project_id = @project_id AND
    ee.environment_id = @environment_id
ORDER BY ee.name ASC
FOR UPDATE OF ee;

-- name: DeleteEnvironment :one
WITH deleted_env AS (
    UPDATE environments
    SET deleted_at = now()
    WHERE environments.slug = $1 AND environments.project_id = $2 AND environments.deleted IS FALSE
    RETURNING id, name, slug, project_id
), cleared_toolsets AS (
    UPDATE toolsets
    SET default_environment_slug = NULL
    FROM deleted_env
    WHERE toolsets.default_environment_slug = deleted_env.slug AND toolsets.project_id = deleted_env.project_id
    RETURNING toolsets.id
)
SELECT id, name, slug, project_id
FROM deleted_env;

-- name: CreateEnvironmentEntries :many
INSERT INTO environment_entries (
    environment_id,
    name,
    value,
    is_secret
)
/*
 Parameters:
 - environment_id: uuid
 - names: text[]
 - values: text[]
 - is_secrets: boolean[]
*/
VALUES (
    @environment_id::uuid,
    unnest(@names::text[]),
    unnest(@values::text[]),
    unnest(@is_secrets::boolean[])
)
RETURNING *;

-- name: CloneEnvironmentEntriesWithValues :exec
-- Copy (name, value, is_secret) triples from a source environment to a new environment.
-- Secret values are ciphertext and flow row-to-row inside Postgres without the
-- application decrypting them. Same plaintext + same nonce + same key produces the
-- same ciphertext under AES-GCM, which is cryptographically permissible. Non-secret
-- values are plaintext and copy verbatim.
INSERT INTO environment_entries (environment_id, name, value, is_secret)
SELECT @new_environment_id::uuid, ee.name, ee.value, ee.is_secret
FROM environment_entries ee
INNER JOIN environments e ON ee.environment_id = e.id
WHERE ee.environment_id = @source_environment_id::uuid
  AND e.project_id = @project_id::uuid;

-- name: CloneEnvironmentEntryNames :exec
-- Copy only the variable names from a source environment, using a caller-supplied
-- placeholder ciphertext as the value for every new entry. Used when the user wants
-- the structure of the source environment but not its secrets. Every placeholder row
-- is marked secret regardless of the source flag: the placeholder is ciphertext, and
-- the is_secret ⇔ ciphertext invariant must hold. Users flip the flag when they fill
-- in the real value.
INSERT INTO environment_entries (environment_id, name, value, is_secret)
SELECT @new_environment_id::uuid, ee.name, @placeholder_value::text, TRUE
FROM environment_entries ee
INNER JOIN environments e ON ee.environment_id = e.id
WHERE ee.environment_id = @source_environment_id::uuid
  AND e.project_id = @project_id::uuid;

-- name: UpsertEnvironmentEntry :one
INSERT INTO environment_entries (environment_id, name, value, is_secret, updated_at)
SELECT e.id, @name::text, @value::text, @is_secret::boolean, now()
FROM environments e
WHERE e.id = @environment_id AND e.project_id = @project_id
ON CONFLICT (environment_id, name)
DO UPDATE SET
    value = EXCLUDED.value,
    is_secret = EXCLUDED.is_secret,
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
-- Writes nothing (no row returned) unless the environment is live in this
-- project.
INSERT INTO source_environments (
    source_kind,
    source_slug,
    project_id,
    environment_id
)
SELECT @source_kind, @source_slug, e.project_id, e.id
FROM environments e
WHERE e.id = @environment_id
  AND e.project_id = @project_id
  AND e.deleted IS FALSE
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
-- Writes nothing (no row returned) unless the toolset is live in this
-- project, the environment is live in the same project, and never rewrites a
-- binding that belongs to another project.
INSERT INTO toolset_environments (
    toolset_id,
    project_id,
    environment_id
)
SELECT t.id, t.project_id, @environment_id
FROM toolsets t
WHERE t.id = @toolset_id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
  AND EXISTS (
    SELECT 1
    FROM environments e
    WHERE e.id = @environment_id
      AND e.project_id = @project_id
      AND e.deleted IS FALSE
  )
ON CONFLICT (toolset_id)
DO UPDATE SET
    environment_id = EXCLUDED.environment_id,
    updated_at = now()
WHERE toolset_environments.project_id = EXCLUDED.project_id
RETURNING *;

-- name: DeleteToolsetEnvironment :exec
DELETE FROM toolset_environments
WHERE toolset_id = @toolset_id AND project_id = @project_id;

-- name: LockSourceEnvironmentBinding :one
-- The environment a source is bound to, as stored (the environment may since
-- have been deleted), locked for the caller's link change.
SELECT environment_id
FROM source_environments
WHERE source_kind = @source_kind
  AND source_slug = @source_slug
  AND project_id = @project_id
FOR UPDATE;

-- name: LockToolsetEnvironmentBinding :one
-- The toolset counterpart of LockSourceEnvironmentBinding.
SELECT environment_id
FROM toolset_environments
WHERE toolset_id = @toolset_id
  AND project_id = @project_id
FOR UPDATE;

-- name: ListMCPHeaderEnvironmentEntries :many
-- Reads a live environment of a project together with only its MCP_HEADER_
-- entries, in one statement so a concurrent delete yields either the earlier
-- mapping or no environment row, never a live environment missing its
-- entries. An environment with no such entries yields one row with NULL entry
-- columns; a deleted, missing or foreign environment yields no rows.
SELECT
    e.id AS environment_id,
    e.name AS environment_name,
    e.slug AS environment_slug,
    ee.name AS entry_name,
    ee.value AS entry_value,
    ee.is_secret AS entry_is_secret
FROM environments e
LEFT JOIN environment_entries ee
    ON ee.environment_id = e.id AND ee.name LIKE 'MCP\_HEADER\_%'
WHERE e.id = @environment_id
  AND e.project_id = @project_id
  AND e.deleted IS FALSE
ORDER BY ee.name;

-- name: ListMCPHeaderNearMissEntryNames :many
-- Names, never values, of entries in a live environment of a project that look
-- like an attempt at the MCP_HEADER_ prefix but do not match it exactly.
SELECT ee.name
FROM environment_entries ee
JOIN environments e ON e.id = ee.environment_id
WHERE e.id = @environment_id
  AND e.project_id = @project_id
  AND e.deleted IS FALSE
  AND ee.name NOT LIKE 'MCP\_HEADER\_%'
  AND (LOWER(ee.name) LIKE 'mcp\_header\_%' OR LOWER(ee.name) LIKE 'header\_%')
ORDER BY ee.name;

-- name: GetMCPServerHeaderSnapshot :many
-- Reads, in one statement, an MCP server's current backend, the URL of its
-- remote source, its environment link and only the MCP_HEADER_ entries of
-- that environment. A caller compares the backend, URL and link with the row
-- it already authorized the request against, so the headers it sends and the
-- destination it dials belong to one configuration that existed at one
-- instant. A missing or deleted server yields no rows; a server whose linked
-- environment is deleted, missing or foreign yields NULL environment columns.
SELECT
    s.environment_id AS server_environment_id,
    s.remote_mcp_server_id AS server_remote_mcp_server_id,
    s.tunneled_mcp_server_id AS server_tunneled_mcp_server_id,
    r.url AS remote_url,
    e.id AS live_environment_id,
    ee.name AS entry_name,
    ee.value AS entry_value,
    ee.is_secret AS entry_is_secret
FROM mcp_servers s
LEFT JOIN remote_mcp_servers r
    ON r.id = s.remote_mcp_server_id AND r.project_id = s.project_id AND r.deleted IS FALSE
LEFT JOIN environments e
    ON e.id = s.environment_id AND e.project_id = s.project_id AND e.deleted IS FALSE
LEFT JOIN environment_entries ee
    ON ee.environment_id = e.id AND ee.name LIKE 'MCP\_HEADER\_%'
WHERE s.id = @mcp_server_id
  AND s.project_id = @project_id
  AND s.deleted IS FALSE
ORDER BY ee.name;

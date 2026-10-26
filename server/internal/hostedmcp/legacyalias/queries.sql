-- The legacy /mcp/<slug> alias of toolsets created before hosted servers owned their endpoints (AIM-670). These
-- are the only queries that name toolsets.mcp_slug and toolsets.custom_domain_id; every other sqlc package reads
-- toolsets through sqlc_retired_columns.sql, which drops the toolset hosting columns.

-- name: FindToolsetByAlias :one
-- Loose platform lookup: prefers a platform alias but accepts a custom-domain alias with the slug.
SELECT id, project_id, mcp_slug, custom_domain_id
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND deleted IS FALSE
ORDER BY (custom_domain_id IS NULL) DESC
LIMIT 1;

-- name: FindToolsetByPlatformAlias :one
SELECT id, project_id, mcp_slug, custom_domain_id
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND custom_domain_id IS NULL
  AND deleted IS FALSE;

-- name: FindToolsetByCustomDomainAlias :one
SELECT id, project_id, mcp_slug, custom_domain_id
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND custom_domain_id = @custom_domain_id
  AND deleted IS FALSE;

-- name: FindToolsetByProjectAlias :one
SELECT id, project_id, mcp_slug, custom_domain_id
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetAlias :one
SELECT mcp_slug, custom_domain_id
FROM toolsets
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ListAliases :many
-- The live toolsets in the project that carry an alias.
SELECT id, mcp_slug, custom_domain_id
FROM toolsets
WHERE project_id = @project_id
  AND id = ANY(@ids::uuid[])
  AND mcp_slug IS NOT NULL
  AND deleted IS FALSE;

-- name: AliasTaken :one
-- Whether a live toolset alias holds the slug in the namespace (platform when custom_domain_id is NULL). Not
-- project-scoped, mirroring the partial unique index. exclude_toolset_id discounts that toolset's alias;
-- exclude_mcp_server_id discounts the toolset backing that server.
SELECT EXISTS (
  SELECT 1
  FROM toolsets t
  WHERE t.mcp_slug = @slug
    AND t.custom_domain_id IS NOT DISTINCT FROM sqlc.narg('custom_domain_id')::uuid
    AND t.deleted IS FALSE
    AND (sqlc.narg('exclude_toolset_id')::uuid IS NULL OR t.id <> sqlc.narg('exclude_toolset_id')::uuid)
    AND (
      sqlc.narg('exclude_mcp_server_id')::uuid IS NULL
      OR NOT EXISTS (
        SELECT 1
        FROM mcp_servers s
        WHERE s.id = sqlc.narg('exclude_mcp_server_id')::uuid
          AND s.toolset_id = t.id
      )
    )
)::boolean AS taken;

-- name: ListTakenAliases :many
-- Which of the given addresses, keyed "<slug>|<custom domain id or empty for the platform>", a live toolset alias holds.
SELECT DISTINCT (t.mcp_slug || '|' || COALESCE(t.custom_domain_id::text, ''))::text AS address_key
FROM toolsets t
WHERE t.mcp_slug = ANY(@slugs::text[])
  AND (t.mcp_slug || '|' || COALESCE(t.custom_domain_id::text, '')) = ANY(@address_keys::text[])
  AND t.deleted IS FALSE;

-- name: MoveAlias :exec
-- Moves a legacy toolset's alias to the hosted endpoint that now holds its address.
UPDATE toolsets
SET
    mcp_slug = @mcp_slug
  , custom_domain_id = @custom_domain_id
  , updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE AND mcp_slug IS NOT NULL;

-- name: SetAliasFixture :exec
UPDATE toolsets
SET mcp_slug = sqlc.narg('mcp_slug')::text, custom_domain_id = sqlc.narg('custom_domain_id')::uuid, updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id;

-- The legacy /mcp/<slug> alias of toolsets created before hosted servers owned their endpoints (AIM-670). These
-- are the only queries that name toolsets.mcp_slug and toolsets.custom_domain_id; every other sqlc package reads
-- toolsets through sqlc_retired_columns.sql, which drops the toolset hosting columns.

-- name: FindToolsetByAlias :one
-- Loose platform lookup: prefers a platform alias but accepts a custom-domain alias with the slug.
-- A winning row in a deleted project still owns the slug, so it yields no row rather than a fallback.
SELECT id, project_id, mcp_slug, custom_domain_id
FROM toolsets
WHERE id = (
    SELECT t.id
    FROM toolsets t
    WHERE t.mcp_slug = @mcp_slug
      AND t.deleted IS FALSE
    ORDER BY (t.custom_domain_id IS NULL) DESC
    LIMIT 1
  )
  AND EXISTS (SELECT 1 FROM projects p WHERE p.id = toolsets.project_id AND p.organization_id = toolsets.organization_id AND p.deleted IS FALSE);

-- name: FindToolsetByPlatformAlias :one
SELECT id, project_id, mcp_slug, custom_domain_id
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND custom_domain_id IS NULL
  AND deleted IS FALSE
  AND EXISTS (SELECT 1 FROM projects p WHERE p.id = toolsets.project_id AND p.organization_id = toolsets.organization_id AND p.deleted IS FALSE);

-- name: FindToolsetByCustomDomainAlias :one
SELECT id, project_id, mcp_slug, custom_domain_id
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND custom_domain_id = @custom_domain_id
  AND deleted IS FALSE
  AND EXISTS (SELECT 1 FROM projects p WHERE p.id = toolsets.project_id AND p.organization_id = toolsets.organization_id AND p.deleted IS FALSE);

-- name: FindToolsetByProjectAlias :one
-- A platform alias wins over custom-domain aliases with the same slug.
SELECT id, project_id, mcp_slug, custom_domain_id
FROM toolsets
WHERE mcp_slug = @mcp_slug
  AND project_id = @project_id
  AND deleted IS FALSE
  AND EXISTS (SELECT 1 FROM projects p WHERE p.id = toolsets.project_id AND p.organization_id = toolsets.organization_id AND p.deleted IS FALSE)
ORDER BY (custom_domain_id IS NULL) DESC, created_at, id
LIMIT 1;

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
-- exclude_mcp_server_id discounts the toolset backing that server. On the platform, a custom-domain alias in a
-- live project also holds the slug while no platform alias does, since FindToolsetByAlias serves it there; only
-- that toolset or its canonical server may claim it.
SELECT (EXISTS (
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
) OR (
  sqlc.narg('custom_domain_id')::uuid IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM toolsets pa
    WHERE pa.mcp_slug = @slug
      AND pa.custom_domain_id IS NULL
      AND pa.deleted IS FALSE
  )
  AND EXISTS (
    SELECT 1
    FROM toolsets t
    JOIN projects p ON p.id = t.project_id AND p.deleted IS FALSE
    WHERE t.mcp_slug = @slug
      AND t.custom_domain_id IS NOT NULL
      AND t.deleted IS FALSE
      AND (sqlc.narg('exclude_toolset_id')::uuid IS NULL OR t.id <> sqlc.narg('exclude_toolset_id')::uuid)
      AND (
        sqlc.narg('exclude_mcp_server_id')::uuid IS NULL
        OR sqlc.narg('exclude_mcp_server_id')::uuid <> t.id
        OR NOT EXISTS (
          SELECT 1
          FROM mcp_servers s
          WHERE s.id = t.id
            AND s.toolset_id = t.id
        )
      )
  )
))::boolean AS taken;

-- name: ListTakenAliases :many
-- Which of the given addresses, keyed "<slug>|<custom domain id or empty for the platform>", a live toolset alias holds.
-- The platform host also serves a custom-domain alias in a live project.
SELECT DISTINCT (t.mcp_slug || '|' || COALESCE(t.custom_domain_id::text, ''))::text AS address_key
FROM toolsets t
WHERE t.mcp_slug = ANY(@slugs::text[])
  AND (t.mcp_slug || '|' || COALESCE(t.custom_domain_id::text, '')) = ANY(@address_keys::text[])
  AND t.deleted IS FALSE
UNION
SELECT (t.mcp_slug || '|')::text AS address_key
FROM toolsets t
JOIN projects p ON p.id = t.project_id AND p.deleted IS FALSE
WHERE t.mcp_slug = ANY(@slugs::text[])
  AND (t.mcp_slug || '|') = ANY(@address_keys::text[])
  AND t.custom_domain_id IS NOT NULL
  AND t.deleted IS FALSE;

-- name: ListPlatformServedAliases :many
-- Canonical hosted servers whose toolset alias sits on a custom domain and is the only alias with its slug, with no
-- live platform endpoint holding it: the loose legacy lookup serves that alias on the platform host too.
SELECT t.id AS mcp_server_id, t.mcp_slug::text AS slug
FROM toolsets t
JOIN projects p
  ON p.id = t.project_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
JOIN mcp_servers s
  ON s.id = t.id
  AND s.project_id = t.project_id
  AND s.toolset_id = t.id
  AND s.deleted IS FALSE
WHERE t.id = ANY(@mcp_server_ids::uuid[])
  AND t.project_id = ANY(@project_ids::uuid[])
  AND t.organization_id = @organization_id
  AND t.mcp_slug IS NOT NULL
  AND t.custom_domain_id IS NOT NULL
  AND t.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM toolsets o
    WHERE o.mcp_slug = t.mcp_slug
      AND o.custom_domain_id IS NULL
      AND o.deleted IS FALSE
  )
  AND NOT EXISTS (
    SELECT 1
    FROM toolsets o
    WHERE o.mcp_slug = t.mcp_slug
      AND o.custom_domain_id IS NOT NULL
      AND o.id <> t.id
      AND o.deleted IS FALSE
  )
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_endpoints e
    WHERE e.slug = t.mcp_slug
      AND e.custom_domain_id IS NULL
      AND e.deleted IS FALSE
  );

-- name: OtherCustomDomainAliasServedOnPlatform :one
-- Whether another live-project toolset's custom-domain alias holds the slug while no platform alias does: the
-- loose platform lookup serves that alias, so a second custom-domain alias with the slug would contend for it.
SELECT (
  NOT EXISTS (
    SELECT 1
    FROM toolsets pa
    WHERE pa.mcp_slug = @slug
      AND pa.custom_domain_id IS NULL
      AND pa.deleted IS FALSE
      AND pa.id <> @toolset_id
  )
  AND EXISTS (
    SELECT 1
    FROM toolsets t
    JOIN projects p ON p.id = t.project_id AND p.deleted IS FALSE
    WHERE t.mcp_slug = @slug
      AND t.custom_domain_id IS NOT NULL
      AND t.deleted IS FALSE
      AND t.id <> @toolset_id
  )
)::boolean AS served;

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

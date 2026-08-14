-- name: GetProjectByID :one
SELECT id, slug
FROM projects
WHERE id = @id
  AND deleted IS FALSE;

-- name: GetProjectBySlug :one
SELECT id, slug
FROM projects
WHERE slug = @slug
  AND deleted IS FALSE;

-- name: AdminGetProjectDetailByID :one
SELECT
    p.id,
    p.name,
    p.slug,
    p.organization_id,
    p.logo_asset_id,
    p.functions_runner_version,
    p.created_at,
    p.updated_at,
    (SELECT count(*) FROM toolsets t WHERE t.project_id = p.id AND t.deleted IS FALSE)::bigint AS toolset_count,
    (SELECT count(*) FROM deployments d WHERE d.project_id = p.id)::bigint AS deployment_count,
    (SELECT count(*) FROM http_tool_definitions h WHERE h.project_id = p.id AND h.deleted IS FALSE)::bigint AS http_tool_count,
    (SELECT count(*) FROM environments e WHERE e.project_id = p.id AND e.deleted IS FALSE)::bigint AS environment_count,
    (SELECT count(*) FROM api_keys k WHERE k.project_id = p.id AND k.deleted IS FALSE)::bigint AS api_key_count,
    (SELECT count(*) FROM assistants a WHERE a.project_id = p.id AND a.deleted IS FALSE)::bigint AS assistant_count
FROM projects p
WHERE p.id = @id
  AND p.deleted IS FALSE;

-- name: AdminGetProjectDetailBySlug :one
SELECT
    p.id,
    p.name,
    p.slug,
    p.organization_id,
    p.logo_asset_id,
    p.functions_runner_version,
    p.created_at,
    p.updated_at,
    (SELECT count(*) FROM toolsets t WHERE t.project_id = p.id AND t.deleted IS FALSE)::bigint AS toolset_count,
    (SELECT count(*) FROM deployments d WHERE d.project_id = p.id)::bigint AS deployment_count,
    (SELECT count(*) FROM http_tool_definitions h WHERE h.project_id = p.id AND h.deleted IS FALSE)::bigint AS http_tool_count,
    (SELECT count(*) FROM environments e WHERE e.project_id = p.id AND e.deleted IS FALSE)::bigint AS environment_count,
    (SELECT count(*) FROM api_keys k WHERE k.project_id = p.id AND k.deleted IS FALSE)::bigint AS api_key_count,
    (SELECT count(*) FROM assistants a WHERE a.project_id = p.id AND a.deleted IS FALSE)::bigint AS assistant_count
FROM projects p
WHERE p.slug = @slug
  AND p.deleted IS FALSE;

-- name: AdminListOrganizations :many
-- Two paging modes share this query. A caller that supplies no sort key gets the
-- cursor walk it always had: the sort ladder collapses to all-NULL and the
-- tiebreaker alone orders the rows. A caller that supplies one gets offset paging.
WITH filtered AS (
    SELECT
        om.id,
        om.name,
        om.slug,
        om.gram_account_type AS account_type,
        om.workos_id,
        om.whitelisted,
        om.disabled_at,
        om.free_trial_started_at,
        om.free_trial_ends_at,
        -- converted/demoted precede the dates: those rows keep an ends_at that would otherwise read as running or expired.
        CASE
            WHEN t.organization_id IS NULL THEN 'none'
            WHEN t.converted_at IS NOT NULL THEN 'converted'
            WHEN t.demoted_at IS NOT NULL THEN 'demoted'
            WHEN t.ends_at <= now() THEN 'expired'
            WHEN t.ends_at <= now() + INTERVAL '7 days' THEN 'ending_soon'
            ELSE 'running'
        END::text AS trial_state,
        t.ends_at AS trial_ends_at,
        om.created_at,
        om.updated_at,
        (
            SELECT count(*)
            FROM organization_user_relationships our
            WHERE our.organization_id = om.id
              AND our.deleted IS FALSE
        )::bigint AS member_count
    FROM organization_metadata om
    LEFT JOIN trials t ON t.organization_id = om.id
    WHERE
        -- The id arms compare exactly because a substring match on an opaque high-cardinality id produces incidental hits an operator cannot explain.
        -- Exactness buys no index here, so do not "restore" one: the ILIKE arms share this OR group and no trigram index exists, so Postgres cannot build a BitmapOr and any non-null q scans the table whatever the id arms do.
        (
            sqlc.narg('q')::text IS NULL
            OR om.name ILIKE '%' || sqlc.narg('q')::text || '%'
            OR om.slug ILIKE '%' || sqlc.narg('q')::text || '%'
            OR om.id = sqlc.narg('q')::text
            OR om.workos_id = sqlc.narg('q')::text
        )
        AND (sqlc.narg('account_type')::text IS NULL OR om.gram_account_type = sqlc.narg('account_type')::text)
        AND (sqlc.arg('include_disabled')::boolean OR om.disabled_at IS NULL)
        AND (sqlc.narg('after_id')::text IS NULL OR om.id > sqlc.narg('after_id')::text)
)
SELECT * FROM filtered
-- The sort key stays a bound parameter, never an interpolated column name, so no
-- caller input reaches the parser. NULLS LAST on every arm keeps empty dates at
-- the bottom whichever way the direction points.
ORDER BY
    CASE WHEN sqlc.arg('sort_by')::text = 'name' AND sqlc.arg('sort_dir')::text = 'asc' THEN name END ASC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'name' AND sqlc.arg('sort_dir')::text = 'desc' THEN name END DESC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'slug' AND sqlc.arg('sort_dir')::text = 'asc' THEN slug END ASC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'slug' AND sqlc.arg('sort_dir')::text = 'desc' THEN slug END DESC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'account_type' AND sqlc.arg('sort_dir')::text = 'asc' THEN account_type END ASC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'account_type' AND sqlc.arg('sort_dir')::text = 'desc' THEN account_type END DESC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'member_count' AND sqlc.arg('sort_dir')::text = 'asc' THEN member_count END ASC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'member_count' AND sqlc.arg('sort_dir')::text = 'desc' THEN member_count END DESC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'created_at' AND sqlc.arg('sort_dir')::text = 'asc' THEN created_at END ASC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'created_at' AND sqlc.arg('sort_dir')::text = 'desc' THEN created_at END DESC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'disabled_at' AND sqlc.arg('sort_dir')::text = 'asc' THEN disabled_at END ASC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'disabled_at' AND sqlc.arg('sort_dir')::text = 'desc' THEN disabled_at END DESC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'trial_ends_at' AND sqlc.arg('sort_dir')::text = 'asc' THEN trial_ends_at END ASC NULLS LAST,
    CASE WHEN sqlc.arg('sort_by')::text = 'trial_ends_at' AND sqlc.arg('sort_dir')::text = 'desc' THEN trial_ends_at END DESC NULLS LAST,
    -- Without this tiebreaker rows that tie on the sort key can swap between calls, which drops or repeats rows across a page boundary.
    id ASC
LIMIT sqlc.arg('page_limit')::int
OFFSET sqlc.arg('page_offset')::bigint;

-- name: AdminCountOrganizations :one
-- The count cannot ride on the page query. That query carries the cursor
-- predicate, so a window count inside it reports the rows after the cursor
-- rather than the rows the filters matched, and a page past the end returns no
-- row to carry a count at all. Keeping it out also leaves the page query free to
-- terminate early on its index scan.
--
-- The filter arms must stay identical to AdminListOrganizations. The trials join
-- is absent on purpose rather than by omission: organization_id is that table's
-- primary key, so the left join there cannot change how many rows match.
SELECT count(*)::bigint
FROM organization_metadata om
WHERE
    (
        sqlc.narg('q')::text IS NULL
        OR om.name ILIKE '%' || sqlc.narg('q')::text || '%'
        OR om.slug ILIKE '%' || sqlc.narg('q')::text || '%'
        OR om.id = sqlc.narg('q')::text
        OR om.workos_id = sqlc.narg('q')::text
    )
    AND (sqlc.narg('account_type')::text IS NULL OR om.gram_account_type = sqlc.narg('account_type')::text)
    AND (sqlc.arg('include_disabled')::boolean OR om.disabled_at IS NULL);

-- name: AdminUpdateOrganization :exec
-- Admin-only mutation. Both fields are optional — caller passes NULL to skip
-- the field. NULL on both is a no-op (still touches updated_at).
UPDATE organization_metadata
SET
    gram_account_type = COALESCE(sqlc.narg('account_type')::text, gram_account_type),
    whitelisted = COALESCE(sqlc.narg('whitelisted')::boolean, whitelisted),
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: AdminListProjectsForOrganization :many
SELECT id, slug, name, created_at, updated_at
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC
LIMIT 200;

-- name: AdminListOrganizationMembers :many
SELECT
    u.id,
    u.email,
    u.display_name,
    u.last_login,
    u.created_at,
    u.updated_at
FROM organization_user_relationships our
JOIN users u ON u.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.deleted IS FALSE
ORDER BY u.email ASC
LIMIT 200;

-- name: AdminGetOrganizationByIDOrSlug :one
SELECT
    om.id,
    om.name,
    om.slug,
    om.gram_account_type AS account_type,
    om.workos_id,
    om.whitelisted,
    om.disabled_at,
    om.free_trial_started_at,
    om.free_trial_ends_at,
    -- Must stay identical to AdminListOrganizations.
    CASE
        WHEN t.organization_id IS NULL THEN 'none'
        WHEN t.converted_at IS NOT NULL THEN 'converted'
        WHEN t.demoted_at IS NOT NULL THEN 'demoted'
        WHEN t.ends_at <= now() THEN 'expired'
        WHEN t.ends_at <= now() + INTERVAL '7 days' THEN 'ending_soon'
        ELSE 'running'
    END::text AS trial_state,
    t.ends_at AS trial_ends_at,
    om.created_at,
    om.updated_at,
    (
        SELECT count(*)
        FROM organization_user_relationships our
        WHERE our.organization_id = om.id
          AND our.deleted IS FALSE
    )::bigint AS member_count
FROM organization_metadata om
LEFT JOIN trials t ON t.organization_id = om.id
WHERE om.id = sqlc.arg('id_or_slug')::text
   OR om.slug = sqlc.arg('id_or_slug')::text
LIMIT 1;

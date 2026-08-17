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
WITH search AS (
    -- Escaped once here rather than per arm so the name and the slug arm cannot
    -- drift apart. Backslash goes first or it escapes the escapes that follow.
    -- Every id in both id spaces contains underscores and _ is a
    -- single-character wildcard, so an unescaped pasted id draws incidental
    -- matches out of the name and slug arms.
    SELECT
        sqlc.narg('q')::text AS term,
        '%' || replace(replace(replace(sqlc.narg('q')::text, '\', '\\'), '%', '\%'), '_', '\_') || '%' AS pattern
),
filtered AS (
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
    CROSS JOIN search
    WHERE
        -- The id arms compare exactly because a substring match on an opaque high-cardinality id produces incidental hits an operator cannot explain.
        -- They compare case-insensitively because a real WorkOS id embeds an uppercase ULID and a log pipeline hands the operator a lowercased copy of it.
        -- Exactness buys no index here, so do not "restore" one: the ILIKE arms share this OR group and no trigram index exists, so Postgres cannot build a BitmapOr and any non-null q scans the table whatever the id arms do.
        (
            search.term IS NULL
            OR om.name ILIKE search.pattern
            OR om.slug ILIKE search.pattern
            OR lower(om.id) = lower(search.term)
            OR lower(om.workos_id) = lower(search.term)
        )
        -- coalesce, not a bare cardinality: an absent filter reaches pgx as a nil
        -- slice and encodes to a NULL array, and cardinality(NULL) is NULL, which
        -- would drop every row instead of keeping every row.
        AND (coalesce(cardinality(sqlc.arg('account_types')::text[]), 0) = 0 OR om.gram_account_type = ANY(sqlc.arg('account_types')::text[]))
        -- No empty arm: the handler resolves an absent filter to {active}.
        -- The id arms repeat here, and only here, so a pasted id reaches a disabled organization: investigating one is a leading reason to paste an id at all.
        -- Deliberately not repeated on the account type arm or the cursor, which keep applying to an id match.
        AND (
            (CASE WHEN om.disabled_at IS NULL THEN 'active' ELSE 'disabled' END) = ANY(sqlc.arg('disabled_states')::text[])
            OR lower(om.id) = lower(search.term)
            OR lower(om.workos_id) = lower(search.term)
        )
        AND (sqlc.narg('after_id')::text IS NULL OR om.id > sqlc.narg('after_id')::text)
)
SELECT * FROM filtered
-- trial_state is computed in the CTE's select list, so it cannot be named in the
-- CTE's own WHERE. Filtering out here keeps the ladder to one copy per query.
WHERE coalesce(cardinality(sqlc.arg('trial_states')::text[]), 0) = 0 OR trial_state = ANY(sqlc.arg('trial_states')::text[])
-- The sort key stays a bound parameter, never an interpolated column name, so no
-- caller input reaches the parser. NULLS LAST is what keeps empty dates at the
-- bottom under DESC, where Postgres would otherwise put them first; on the ASC
-- arms it only spells out the default. Both are written out so the two arms of a
-- column read alike.
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
-- The filter arms must stay identical to AdminListOrganizations, trial_states
-- included. That arm reads a column only the trials join can produce, so this
-- query carries the join and the same CASE ladder. The join stays count-safe
-- because organization_id is that table's primary key and cannot fan one
-- organization out into several counted rows.
WITH search AS (
    -- Identical to AdminListOrganizations, escaping included: a pasted id that
    -- reaches the rows through an escaped pattern and the total through an
    -- unescaped one gives the pager a count that disagrees with what it shows.
    SELECT
        sqlc.narg('q')::text AS term,
        '%' || replace(replace(replace(sqlc.narg('q')::text, '\', '\\'), '%', '\%'), '_', '\_') || '%' AS pattern
),
filtered AS (
    SELECT
        CASE
            WHEN t.organization_id IS NULL THEN 'none'
            WHEN t.converted_at IS NOT NULL THEN 'converted'
            WHEN t.demoted_at IS NOT NULL THEN 'demoted'
            WHEN t.ends_at <= now() THEN 'expired'
            WHEN t.ends_at <= now() + INTERVAL '7 days' THEN 'ending_soon'
            ELSE 'running'
        END::text AS trial_state
    FROM organization_metadata om
    LEFT JOIN trials t ON t.organization_id = om.id
    CROSS JOIN search
    WHERE
        (
            search.term IS NULL
            OR om.name ILIKE search.pattern
            OR om.slug ILIKE search.pattern
            OR lower(om.id) = lower(search.term)
            OR lower(om.workos_id) = lower(search.term)
        )
        AND (coalesce(cardinality(sqlc.arg('account_types')::text[]), 0) = 0 OR om.gram_account_type = ANY(sqlc.arg('account_types')::text[]))
        AND (
            (CASE WHEN om.disabled_at IS NULL THEN 'active' ELSE 'disabled' END) = ANY(sqlc.arg('disabled_states')::text[])
            OR lower(om.id) = lower(search.term)
            OR lower(om.workos_id) = lower(search.term)
        )
)
SELECT count(*)::bigint FROM filtered
WHERE coalesce(cardinality(sqlc.arg('trial_states')::text[]), 0) = 0 OR trial_state = ANY(sqlc.arg('trial_states')::text[]);

-- name: AdminGetOrganizationStats :one
-- Blind to the list's filters by design: these figures must not move when an
-- operator filters. total and both 7-day windows count disabled organizations
-- too, so the strip reports the real platform size rather than the list's
-- default active-only view.
--
-- The join stays count-safe because organization_id is the trials primary key.
-- Both 7-day windows exclude their boundary: exactly seven days old is outside.
WITH orgs AS (
    SELECT
        om.created_at,
        om.disabled_at,
        -- Must stay identical to AdminListOrganizations: a figure counted from a
        -- shortened predicate would disagree with the rows clicking it lands on.
        CASE
            WHEN t.organization_id IS NULL THEN 'none'
            WHEN t.converted_at IS NOT NULL THEN 'converted'
            WHEN t.demoted_at IS NOT NULL THEN 'demoted'
            WHEN t.ends_at <= now() THEN 'expired'
            WHEN t.ends_at <= now() + INTERVAL '7 days' THEN 'ending_soon'
            ELSE 'running'
        END::text AS trial_state
    FROM organization_metadata om
    LEFT JOIN trials t ON t.organization_id = om.id
)
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE created_at > now() - INTERVAL '7 days')::bigint AS created_last_7_days,
    count(*) FILTER (WHERE trial_state = 'ending_soon')::bigint AS trials_ending_soon,
    count(*) FILTER (WHERE disabled_at IS NOT NULL)::bigint AS disabled,
    count(*) FILTER (WHERE disabled_at > now() - INTERVAL '7 days')::bigint AS disabled_last_7_days
FROM orgs;

-- name: AdminUpdateOrganization :exec
-- Admin-only mutation. Both fields are optional — caller passes NULL to skip
-- the field. NULL on both is a no-op (still touches updated_at).
UPDATE organization_metadata
SET
    gram_account_type = COALESCE(sqlc.narg('account_type')::text, gram_account_type),
    whitelisted = COALESCE(sqlc.narg('whitelisted')::boolean, whitelisted),
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: AdminBulkUpdateAccountType :many
-- One statement rather than a loop, so every id is matched against one snapshot.
-- RETURNING is how the caller learns which of its ids matched nothing.
UPDATE organization_metadata
SET
    gram_account_type = @account_type::text,
    updated_at = clock_timestamp()
WHERE id = ANY(@ids::text[])
RETURNING id;

-- name: AdminDisableOrganization :execrows
-- Operator-initiated disable. Keyed on the Gram organization id rather than
-- workos_id so an organization that was never linked to WorkOS can still be
-- disabled. Deliberately leaves workos_last_event_id alone: that column is the
-- WorkOS webhook cursor and this is not a WorkOS event, so stamping it would
-- misrecord which event was last applied. Idempotent — the COALESCE keeps the
-- original timestamp when the organization is already disabled.
UPDATE organization_metadata
SET disabled_at = COALESCE(disabled_at, clock_timestamp()),
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: AdminEnableOrganization :execrows
-- Undo of AdminDisableOrganization, and likewise blind to workos_last_event_id.
-- Idempotent — enabling an already-active organization is a no-op beyond
-- updated_at.
UPDATE organization_metadata
SET disabled_at = NULL,
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

-- name: AdminGetOrganization :one
-- Resolving a slug is opt-in because every admin write is keyed on id alone.
-- Both columns are bare TEXT, so one organization's slug can equal another's
-- id; a read-after-write that allowed slugs could then describe a different
-- organization than the one just written, and the operator would see a 200
-- reporting the write never happened. Reads that are not following a write pass
-- allow_slug true, which the dashboard relies on. The ORDER BY settles the same
-- collision for those reads: when the argument is one organization's id and
-- another's slug both rows match, and LIMIT 1 on its own would pick either, so
-- the exact id match is sorted first.
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
WHERE om.id = sqlc.arg('id')::text
   OR (sqlc.arg('allow_slug')::boolean AND om.slug = sqlc.arg('id')::text)
ORDER BY (om.id = sqlc.arg('id')::text) DESC
LIMIT 1;

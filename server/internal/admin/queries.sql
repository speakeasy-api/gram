-- name: AdminGetEnterpriseTrialRetryOperationIDs :one
-- The arm audit id is the immutable generation token. Every production trial
-- creation writes exactly one arm audit in the creation transaction; extension
-- writes neither. seq orders generations; id breaks any equal-seq tie.
WITH latest_arm AS (
    SELECT armed.id, armed.seq
    FROM audit_logs AS armed
    WHERE armed.organization_id = @target_organization_id
      AND armed.project_id IS NULL
      AND armed.action = 'organization:enterprise_trial_armed'
      AND armed.subject_id = @target_organization_id
      AND armed.subject_type = 'organization'
    ORDER BY armed.seq DESC, armed.id DESC
    LIMIT 1
), latest_demotion AS (
    SELECT demoted.id, demoted.seq
    FROM audit_logs AS demoted
    JOIN latest_arm ON (demoted.seq, demoted.id) > (latest_arm.seq, latest_arm.id)
    WHERE demoted.organization_id = @target_organization_id
      AND demoted.project_id IS NULL
      AND demoted.action = 'organization:enterprise_trial_demoted'
      AND demoted.subject_id = @target_organization_id
      AND demoted.subject_type = 'organization'
    ORDER BY demoted.seq DESC, demoted.id DESC
    LIMIT 1
), current_rearms AS (
    SELECT rearmed.id, rearmed.seq, rearmed.metadata->>'arm_operation_id' AS arm_operation_id
    FROM audit_logs AS rearmed
    JOIN latest_demotion ON (rearmed.seq, rearmed.id) > (latest_demotion.seq, latest_demotion.id)
    WHERE rearmed.organization_id = @target_organization_id
      AND rearmed.project_id IS NULL
      AND rearmed.action = 'organization:enterprise_trial_rearmed'
      AND rearmed.subject_id = @target_organization_id
      AND rearmed.subject_type = 'organization'
)
SELECT
    COALESCE((SELECT id::text FROM latest_arm), '')::text AS arm_operation_id,
    COALESCE((SELECT arm_operation_id FROM current_rearms ORDER BY seq DESC, id DESC LIMIT 1), '')::text AS rearm_arm_operation_id,
    (
        SELECT count(*)
        FROM current_rearms
        JOIN latest_arm ON current_rearms.arm_operation_id = latest_arm.id::text
    )::bigint AS matching_rearm_count;

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

-- name: AdminResolveProjectIDBySlug :one
-- The detail query above counts six child tables for every row it matches, and
-- two of those counts have no index on project_id to use. A project slug is
-- unique only within an organization, so a slug the whole platform uses matches
-- one project per organization, and that cost multiplies by the match count.
-- Resolving the slug to a single id first is what holds it to one project's
-- worth of counting.
--
-- Which project a duplicated slug names is arbitrary either way. The ORDER BY
-- only makes the same call answer the same way twice.
SELECT id
FROM projects
WHERE slug = @slug
  AND deleted IS FALSE
ORDER BY id
LIMIT 1;

-- name: AdminResolveProjectIDBySlugInOrganization :one
-- Scoped by the organization a slug names exactly one project, because the
-- unique index on (organization_id, slug) says so. That is what the resolve
-- above cannot promise.
SELECT id
FROM projects
WHERE organization_id = @organization_id
  AND slug = @slug
  AND deleted IS FALSE;

-- name: AdminResolveOrganizationID :one
-- A caller names an organization the way the URL does, by id or slug, and a
-- project row carries only the id. Resolving to the id first is what lets both
-- project addresses be checked against the same value.
--
-- Both columns are bare TEXT, so one organization's slug can equal another's id
-- and two rows can match. The ORDER BY settles that collision the way
-- AdminGetOrganization already does, with the exact id match first.
SELECT id
FROM organization_metadata
WHERE id = sqlc.arg('id_or_slug')::text
   OR slug = sqlc.arg('id_or_slug')::text
ORDER BY (id = sqlc.arg('id_or_slug')::text) DESC
LIMIT 1;

-- name: LockOrganizationMetadata :one
-- Conversion locks this row only after lifecycle and key advisory locks, before
-- reading eligibility or snapshot data that a concurrent admin update can change.
SELECT id
FROM organization_metadata
WHERE id = @id
FOR UPDATE;

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
        bm.stripe_customer_id,
        bm.stripe_subscription_id,
        om.whitelisted,
        om.disabled_at,
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
        members.member_count
    FROM organization_metadata om
    LEFT JOIN trials t ON t.organization_id = om.id
    LEFT JOIN billing_metadata bm ON bm.organization_id = om.id
    -- Only bounds/member sorting need pre-page counts. Keep the guard inside
    -- the aggregate so generic plans skip membership access too. NULL marks
    -- deferred display counts; real zero-member counts remain zero.
    CROSS JOIN LATERAL (
        SELECT CASE WHEN (sqlc.narg('min_members')::bigint IS NOT NULL OR sqlc.narg('max_members')::bigint IS NOT NULL OR sqlc.arg('sort_by')::text = 'member_count')
                    THEN count(*) END::bigint AS member_count
        FROM organization_user_relationships our
        WHERE (sqlc.narg('min_members')::bigint IS NOT NULL OR sqlc.narg('max_members')::bigint IS NOT NULL OR sqlc.arg('sort_by')::text = 'member_count')
          AND our.organization_id = om.id
          AND our.deleted IS FALSE
    ) members
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
        AND (sqlc.narg('min_members')::bigint IS NULL OR members.member_count >= sqlc.narg('min_members')::bigint)
        AND (sqlc.narg('max_members')::bigint IS NULL OR members.member_count <= sqlc.narg('max_members')::bigint)
        AND (sqlc.narg('created_at_gte')::timestamptz IS NULL OR om.created_at >= sqlc.narg('created_at_gte')::timestamptz)
        AND (sqlc.narg('created_at_lt')::timestamptz IS NULL OR om.created_at < sqlc.narg('created_at_lt')::timestamptz)
        -- Status is strict, including exact organization and WorkOS ID searches.
        AND (
            sqlc.arg('disabled_status')::text = 'all'
            OR (sqlc.arg('disabled_status')::text = 'active' AND om.disabled_at IS NULL)
            OR (sqlc.arg('disabled_status')::text = 'disabled' AND om.disabled_at IS NOT NULL)
        )
        -- Keep ID-shaped cursors compatible, but seek in the default creation
        -- order, not ID order. Resolve the anchor outside the filters so changes
        -- to its account/disabled state do not break an existing cursor. A
        -- deleted or unknown anchor exhausts the walk rather than restarting it.
        AND (
            sqlc.narg('after_id')::text IS NULL
            OR EXISTS (
                SELECT 1 FROM organization_metadata anchor
                WHERE anchor.id = sqlc.narg('after_id')::text
                  AND (om.created_at < anchor.created_at
                       OR (om.created_at = anchor.created_at AND om.id > anchor.id))
            )
        )
),
paged AS MATERIALIZED (
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
OFFSET sqlc.arg('page_offset')::bigint
)
-- MATERIALIZED keeps display-only membership access after LIMIT and OFFSET.
SELECT
    id, name, slug, account_type, workos_id, stripe_customer_id,
    stripe_subscription_id, whitelisted, disabled_at, trial_state,
    trial_ends_at, created_at, updated_at,
    coalesce(member_count, (
        SELECT count(*)
        FROM organization_user_relationships our
        WHERE our.organization_id = paged.id
          AND our.deleted IS FALSE
    ))::bigint AS member_count
FROM paged
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
    id ASC;

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
    -- Match the list's active-membership definition without multiplying rows.
    -- Guard access inside the aggregate: generic plans must not scan members
    -- when both bounds are absent.
    CROSS JOIN LATERAL (
        SELECT count(*)::bigint AS member_count
        FROM organization_user_relationships our
        WHERE (sqlc.narg('min_members')::bigint IS NOT NULL OR sqlc.narg('max_members')::bigint IS NOT NULL)
          AND our.organization_id = om.id
          AND our.deleted IS FALSE
    ) members
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
        AND (sqlc.narg('min_members')::bigint IS NULL OR members.member_count >= sqlc.narg('min_members')::bigint)
        AND (sqlc.narg('max_members')::bigint IS NULL OR members.member_count <= sqlc.narg('max_members')::bigint)
        AND (sqlc.narg('created_at_gte')::timestamptz IS NULL OR om.created_at >= sqlc.narg('created_at_gte')::timestamptz)
        AND (sqlc.narg('created_at_lt')::timestamptz IS NULL OR om.created_at < sqlc.narg('created_at_lt')::timestamptz)
        -- Status is strict, including exact organization and WorkOS ID searches.
        AND (
            sqlc.arg('disabled_status')::text = 'all'
            OR (sqlc.arg('disabled_status')::text = 'active' AND om.disabled_at IS NULL)
            OR (sqlc.arg('disabled_status')::text = 'disabled' AND om.disabled_at IS NOT NULL)
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
--
-- A customer is an organization on a paid account type, payg or enterprise. The
-- pair is the same one the organizations service uses to tell a paying account
-- from the rest, and the customer figures count disabled customers too.
WITH orgs AS (
    SELECT
        om.created_at,
        om.disabled_at,
        om.gram_account_type IN ('payg', 'enterprise') AS is_customer,
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
    count(*) FILTER (WHERE is_customer)::bigint AS customers,
    count(*) FILTER (WHERE is_customer AND created_at > now() - INTERVAL '7 days')::bigint AS customers_created_last_7_days,
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

-- name: LockEnterpriseTrialInOrganizations :one
SELECT organization_id
FROM trials
WHERE organization_id = ANY(@ids::text[])
  AND tier = 'enterprise'
ORDER BY organization_id
LIMIT 1
FOR UPDATE;

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
-- Not a plain sum: once AGE-1880 copies legacy servers into mcp_servers, a
-- toolset and its mcp_servers row would each be counted. The anti join on the
-- legacy half gives the same answer before and after that copy, and it is the
-- direction that plans as an index anti join rather than a seq scan of toolsets.
SELECT
    p.id,
    p.slug,
    p.name,
    p.created_at,
    p.updated_at,
    ((SELECT count(*) FROM toolsets t
        WHERE t.project_id = p.id AND t.deleted IS FALSE AND t.mcp_enabled IS TRUE
          AND NOT EXISTS (SELECT 1 FROM mcp_servers m2
                           WHERE m2.toolset_id = t.id AND m2.deleted IS FALSE))
     + (SELECT count(*) FROM mcp_servers m
          WHERE m.project_id = p.id AND m.deleted IS FALSE))::bigint AS mcp_server_count
FROM projects p
WHERE p.organization_id = @organization_id
  AND p.deleted IS FALSE
ORDER BY p.created_at DESC
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
    bm.stripe_customer_id,
    bm.stripe_subscription_id,
    om.whitelisted,
    om.disabled_at,
    -- The lifecycle state calculation must stay identical to AdminListOrganizations.
    CASE
        WHEN t.organization_id IS NULL THEN 'none'
        WHEN t.converted_at IS NOT NULL THEN 'converted'
        WHEN t.demoted_at IS NOT NULL THEN 'demoted'
        WHEN t.ends_at <= now() THEN 'expired'
        WHEN t.ends_at <= now() + INTERVAL '7 days' THEN 'ending_soon'
        ELSE 'running'
    END::text AS trial_state,
    t.tier AS trial_tier,
    t.ends_at AS trial_ends_at,
    t.converted_at AS trial_converted_at,
    t.demoted_at AS trial_demoted_at,
    om.creation_source,
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
LEFT JOIN billing_metadata bm ON bm.organization_id = om.id
WHERE om.id = sqlc.arg('id')::text
   OR (sqlc.arg('allow_slug')::boolean AND om.slug = sqlc.arg('id')::text)
ORDER BY (om.id = sqlc.arg('id')::text) DESC
LIMIT 1;

-- name: AdminSetStripeCustomer :one
INSERT INTO billing_metadata (organization_id, stripe_customer_id)
VALUES (sqlc.arg('organization_id')::text, sqlc.arg('stripe_customer_id')::text)
ON CONFLICT (organization_id) DO UPDATE
SET
    stripe_customer_id = EXCLUDED.stripe_customer_id,
    updated_at = clock_timestamp()
WHERE billing_metadata.stripe_customer_id IS NULL
  AND billing_metadata.stripe_subscription_id IS NULL
RETURNING organization_id;

-- name: LockSupportMatrix :exec
SELECT pg_advisory_xact_lock(719438201);

-- name: SeedSupportPlatforms :exec
INSERT INTO support_matrix_platforms (slug, name, vendor, family, surface, sort_order)
SELECT value->>'id', value->>'name', value->>'vendor', value->>'family', value->>'surface', ordinality::integer
FROM jsonb_array_elements(sqlc.arg(catalog)::jsonb->'products') WITH ORDINALITY
ON CONFLICT (slug) DO NOTHING;

-- name: SeedSupportMethods :exec
INSERT INTO support_matrix_integration_methods (slug, name, vendor, plan_notes, sort_order)
SELECT value->>'id', value->>'name', value->>'vendor', value->>'plans', ordinality::integer
FROM jsonb_array_elements(sqlc.arg(catalog)::jsonb->'methods') WITH ORDINALITY
ON CONFLICT (slug) DO NOTHING;

-- name: SeedSupportCapabilities :exec
INSERT INTO support_matrix_capabilities (slug, name, category, sort_order)
SELECT value->>'id', value->>'name', value->>'group', ordinality::integer
FROM jsonb_array_elements(sqlc.arg(catalog)::jsonb->'capabilities') WITH ORDINALITY
ON CONFLICT (slug) DO NOTHING;

-- name: SeedSupportReferences :exec
INSERT INTO support_matrix_method_capabilities (integration_method_id, capability_id, status, notes, needs_verification)
SELECT m.id, c.id, f.value->>'status', f.value->>'note', (f.value->>'verify')::boolean
FROM jsonb_array_elements(sqlc.arg(catalog)::jsonb->'methods') AS source
CROSS JOIN LATERAL jsonb_each(source->'facts') AS f
JOIN support_matrix_integration_methods m ON m.slug = source->>'id'
JOIN support_matrix_capabilities c ON c.slug = f.key
ON CONFLICT (integration_method_id, capability_id) DO NOTHING;

-- name: ReadSupportMatrix :one
WITH reference_facts AS (
  SELECT m.slug AS method_slug, jsonb_object_agg(c.slug, jsonb_build_object('status', r.status, 'note', r.notes, 'verify', r.needs_verification)) AS facts
  FROM support_matrix_method_capabilities r
  JOIN support_matrix_integration_methods m ON m.id = r.integration_method_id AND m.deleted_at IS NULL
  JOIN support_matrix_capabilities c ON c.id = r.capability_id AND c.deleted_at IS NULL
  WHERE r.deleted_at IS NULL GROUP BY m.slug
), coverage_facts AS (
  SELECT f.method_platform_id, jsonb_object_agg(c.slug, jsonb_build_object('status', f.status, 'note', f.notes, 'verify', f.needs_verification)) AS facts
  FROM support_matrix_coverage f
  JOIN support_matrix_capabilities c ON c.id = f.capability_id AND c.deleted_at IS NULL
  WHERE f.deleted_at IS NULL GROUP BY f.method_platform_id
), mappings AS (
  SELECT m.slug || '/' || p.slug AS key,
    jsonb_build_object('applicability', mp.applicability, 'conditions', mp.conditions, 'facts', coalesce(cf.facts, '{}'::jsonb)) AS value
  FROM support_matrix_method_platforms mp
  JOIN support_matrix_integration_methods m ON m.id = mp.integration_method_id AND m.deleted_at IS NULL
  JOIN support_matrix_platforms p ON p.id = mp.platform_id AND p.deleted_at IS NULL
  LEFT JOIN coverage_facts cf ON cf.method_platform_id = mp.id
  WHERE mp.deleted_at IS NULL
)
SELECT jsonb_build_object(
 'methods', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', m.slug, 'name', m.name, 'vendor', m.vendor, 'plans', m.plan_notes, 'facts', coalesce(r.facts, '{}'::jsonb)) ORDER BY m.sort_order, m.slug), '[]'::jsonb) FROM support_matrix_integration_methods m LEFT JOIN reference_facts r ON r.method_slug = m.slug WHERE m.deleted_at IS NULL),
 'products', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', p.slug, 'name', p.name, 'vendor', p.vendor, 'family', p.family, 'surface', p.surface) ORDER BY p.sort_order, p.slug), '[]'::jsonb) FROM support_matrix_platforms p WHERE p.deleted_at IS NULL),
 'capabilities', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', c.slug, 'name', c.name, 'group', c.category) ORDER BY c.sort_order, c.slug), '[]'::jsonb) FROM support_matrix_capabilities c WHERE c.deleted_at IS NULL),
 'draft', jsonb_build_object('mappings', (SELECT coalesce(jsonb_object_agg(key, value), '{}'::jsonb) FROM mappings), 'references', (SELECT coalesce(jsonb_object_agg(method_slug, facts), '{}'::jsonb) FROM reference_facts))
)::jsonb AS snapshot;

-- name: UpsertSupportMapping :one
INSERT INTO support_matrix_method_platforms (integration_method_id, platform_id, applicability, conditions)
SELECT m.id, p.id, sqlc.arg(applicability)::text, sqlc.arg(conditions)::text
FROM support_matrix_integration_methods m, support_matrix_platforms p
WHERE m.slug = sqlc.arg(method_slug)::text AND p.slug = sqlc.arg(platform_slug)::text AND m.deleted_at IS NULL AND p.deleted_at IS NULL
ON CONFLICT (integration_method_id, platform_id) DO UPDATE SET applicability = EXCLUDED.applicability, conditions = EXCLUDED.conditions, updated_at = clock_timestamp(), deleted_at = NULL
RETURNING id;

-- name: UpsertSupportCoverage :exec
INSERT INTO support_matrix_coverage (method_platform_id, capability_id, status, notes, needs_verification)
SELECT sqlc.arg(mapping_id)::uuid, c.id, sqlc.arg(status)::text, sqlc.arg(notes)::text, sqlc.arg(needs_verification)::boolean
FROM support_matrix_capabilities c WHERE c.slug = sqlc.arg(capability_slug)::text AND c.deleted_at IS NULL
ON CONFLICT (method_platform_id, capability_id) DO UPDATE SET status = EXCLUDED.status, notes = EXCLUDED.notes, needs_verification = EXCLUDED.needs_verification, verified_at = NULL, updated_at = clock_timestamp(), deleted_at = NULL;

-- name: UpsertSupportReference :exec
INSERT INTO support_matrix_method_capabilities (integration_method_id, capability_id, status, notes, needs_verification)
SELECT m.id, c.id, sqlc.arg(status)::text, sqlc.arg(notes)::text, sqlc.arg(needs_verification)::boolean
FROM support_matrix_integration_methods m, support_matrix_capabilities c
WHERE m.slug = sqlc.arg(method_slug)::text AND c.slug = sqlc.arg(capability_slug)::text AND m.deleted_at IS NULL AND c.deleted_at IS NULL
ON CONFLICT (integration_method_id, capability_id) DO UPDATE SET status = EXCLUDED.status, notes = EXCLUDED.notes, needs_verification = EXCLUDED.needs_verification, verified_at = NULL, updated_at = clock_timestamp(), deleted_at = NULL;

-- name: AdminListWorkloadIssuers :many
SELECT id, name, issuer, jwks_uri, project_id, created_at
FROM workload_issuers
WHERE organization_id = sqlc.arg(organization_id)::text AND deleted_at IS NULL
ORDER BY created_at;

-- name: AdminListWorkloadSubjects :many
SELECT
  a.workload_issuer_id,
  a.subject,
  a.name,
  g.agent_id,
  ag.name AS agent_name
FROM workload_identity_admissions a
LEFT JOIN workload_agent_assignments g
  ON g.organization_id = a.organization_id
  AND g.workload_issuer_id = a.workload_issuer_id
  AND g.subject = a.subject
  AND g.deleted_at IS NULL
LEFT JOIN agents ag
  ON ag.organization_id = g.organization_id AND ag.id = g.agent_id AND ag.deleted_at IS NULL
WHERE a.organization_id = sqlc.arg(organization_id)::text AND a.deleted_at IS NULL
ORDER BY a.created_at;

-- name: AdminListWorkloadAuthenticationHosts :many
SELECT id, project_id, use_authentication_host
FROM user_session_issuers
WHERE organization_id = sqlc.arg(organization_id)::text AND deleted_at IS NULL
ORDER BY created_at;

-- name: AdminCreateWorkloadIssuer :one
INSERT INTO workload_issuers (organization_id, project_id, name, issuer, jwks_uri)
VALUES (
  sqlc.arg(organization_id)::text,
  sqlc.narg(project_id)::uuid,
  sqlc.arg(name)::text,
  sqlc.arg(issuer)::text,
  sqlc.arg(jwks_uri)::text
)
RETURNING id;

-- name: AdminAdmitWorkloadSubject :one
INSERT INTO workload_identity_admissions (organization_id, project_id, workload_issuer_id, subject, name)
SELECT
  i.organization_id,
  i.project_id,
  i.id,
  sqlc.arg(subject)::text,
  sqlc.narg(name)::text
FROM workload_issuers i
WHERE i.organization_id = sqlc.arg(organization_id)::text
  AND i.id = sqlc.arg(workload_issuer_id)::uuid
  AND i.deleted_at IS NULL
RETURNING id;

-- name: AdminAssignWorkloadAgent :one
INSERT INTO workload_agent_assignments (organization_id, workload_issuer_id, subject, agent_id)
SELECT
  i.organization_id,
  i.id,
  sqlc.arg(subject)::text,
  sqlc.arg(agent_id)::uuid
FROM workload_issuers i
WHERE i.organization_id = sqlc.arg(organization_id)::text
  AND i.id = sqlc.arg(workload_issuer_id)::uuid
  AND i.deleted_at IS NULL
RETURNING id;

-- name: AdminSetWorkloadAuthenticationHost :execrows
UPDATE user_session_issuers
SET use_authentication_host = sqlc.arg(enabled)::boolean, updated_at = clock_timestamp()
WHERE organization_id = sqlc.arg(organization_id)::text
  AND id = sqlc.arg(user_session_issuer_id)::uuid
  AND deleted_at IS NULL;

-- name: AdminWithdrawWorkloadIssuer :execrows
UPDATE workload_issuers
SET deleted_at = clock_timestamp()
WHERE organization_id = sqlc.arg(organization_id)::text
  AND id = sqlc.arg(workload_issuer_id)::uuid
  AND deleted_at IS NULL;

-- name: AdminWithdrawWorkloadIssuerAdmissions :exec
UPDATE workload_identity_admissions
SET deleted_at = clock_timestamp()
WHERE organization_id = sqlc.arg(organization_id)::text
  AND workload_issuer_id = sqlc.arg(workload_issuer_id)::uuid
  AND deleted_at IS NULL;

-- name: AdminWithdrawWorkloadIssuerAssignments :exec
UPDATE workload_agent_assignments
SET deleted_at = clock_timestamp()
WHERE organization_id = sqlc.arg(organization_id)::text
  AND workload_issuer_id = sqlc.arg(workload_issuer_id)::uuid
  AND deleted_at IS NULL;

-- name: CreateSpendRule :one
-- Inserts a rule version row. version = 1 starts a new lineage; an edit
-- inserts the successor row (same slug, version + 1) after archiving the
-- current row — the partial unique index on live (organization_id, slug)
-- enforces that ordering.
INSERT INTO spend_rules (
    organization_id
  , name
  , slug
  , description
  , target_expr
  , rule_expr
  , limit_usd
  , window_kind
  , warn_at_pct
  , action
  , enabled
  , version
)
VALUES (
    @organization_id
  , @name
  , @slug
  , @description
  , @target_expr
  , @rule_expr
  , @limit_usd
  , @window_kind
  , @warn_at_pct
  , @action
  , @enabled
  , @version
)
RETURNING *;

-- name: SpendRuleSlugExists :one
-- Slugs are reserved permanently, archived lineages included, so a new rule
-- never reuses a slug and rule URNs stay globally unique. Do NOT scope this
-- to `archived IS FALSE`.
SELECT EXISTS (
  SELECT 1
  FROM spend_rules
  WHERE organization_id = @organization_id
    AND slug = @slug
) AS taken;

-- name: GetSpendRule :one
-- The live (non-archived) version row by id.
SELECT *
FROM spend_rules
WHERE id = @id
  AND organization_id = @organization_id
  AND archived IS FALSE;

-- name: GetSpendRuleForUpdate :one
SELECT *
FROM spend_rules
WHERE id = @id
  AND organization_id = @organization_id
  AND archived IS FALSE
FOR UPDATE;

-- name: ListSpendRules :many
SELECT *
FROM spend_rules
WHERE organization_id = @organization_id
  AND archived IS FALSE
ORDER BY created_at DESC;

-- name: ListEnabledSpendRules :many
SELECT *
FROM spend_rules
WHERE organization_id = @organization_id
  AND enabled IS TRUE
  AND archived IS FALSE
ORDER BY created_at DESC;

-- name: ListOrganizationsWithEnabledSpendRules :many
SELECT DISTINCT organization_id
FROM spend_rules
WHERE enabled IS TRUE
  AND archived IS FALSE;

-- name: ToggleSpendRuleEnabled :one
-- enabled is the one mutable field on a live version row: it is an
-- operational kill switch, not part of the rule's config snapshot, so
-- toggling it does not create a new version.
UPDATE spend_rules
SET enabled = @enabled
  , updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND archived IS FALSE
RETURNING *;

-- name: ArchiveSpendRule :one
-- Ends a version row's live tenure. Called on admin archive (no successor)
-- and as the first step of an edit, before the successor row is inserted.
UPDATE spend_rules
SET archived_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND archived IS FALSE
RETURNING *;

-- name: GetArchivedSpendRule :one
-- An archived version row by id, used to inspect lineage links (tests and
-- historical lookups). Live rows go through GetSpendRule.
SELECT *
FROM spend_rules
WHERE id = @id
  AND organization_id = @organization_id
  AND archived IS TRUE;

-- name: SetSpendRuleSupersededBy :exec
-- Links an archived version row to the successor an edit created. Runs after
-- the successor insert because the foreign key requires the target to exist.
UPDATE spend_rules
SET superseded_by = @superseded_by
WHERE id = @id
  AND organization_id = @organization_id;

-- name: InsertSpendRuleEvent :execrows
INSERT INTO spend_rule_events (
    organization_id
  , spend_rule_id
  , rule_urn
  , event_type
  , user_id
  , email
  , display_name
  , spend_usd
  , limit_usd
  , window_start
  , window_end
)
VALUES (
    @organization_id
  , @spend_rule_id
  , @rule_urn
  , @event_type
  , sqlc.narg(user_id)::text
  , @email
  , sqlc.narg(display_name)::text
  , @spend_usd
  , @limit_usd
  , @window_start
  , @window_end
)
ON CONFLICT (spend_rule_id, event_type, email, window_start) DO NOTHING;

-- name: ListSpendRuleEvents :many
-- Events join the exact (immutable) rule version row that fired them, so
-- rule_name and rule config are as of firing time. The optional rule filter
-- expands to the whole slug lineage: pass any version row's id and events
-- from every version of that rule are returned.
SELECT ev.*, r.name AS rule_name
FROM spend_rule_events ev
INNER JOIN spend_rules r ON r.id = ev.spend_rule_id
WHERE ev.organization_id = @organization_id
  AND (
    sqlc.narg(spend_rule_id)::uuid IS NULL
    OR r.slug = (
      SELECT lineage.slug
      FROM spend_rules lineage
      WHERE lineage.id = sqlc.narg(spend_rule_id)::uuid
        AND lineage.organization_id = @organization_id
    )
  )
  AND (sqlc.narg(event_type)::text IS NULL OR ev.event_type = sqlc.narg(event_type)::text)
  AND (sqlc.narg(cursor_id)::uuid IS NULL OR ev.id < sqlc.narg(cursor_id)::uuid)
ORDER BY ev.id DESC
LIMIT @page_limit;

-- name: ListOrgActors :many
-- One row per organization member: identity from users, enriched with the
-- member's directory profile (attributes + group names) when one is synced,
-- and their organization role slugs (e.g. 'admin', 'member'). Members without
-- a directory profile or role assignments still appear with empty
-- attributes/groups/roles so email- and role-based rules can target them.
SELECT
    u.id AS user_id
  , u.email
  , u.display_name
  , COALESCE(du.attributes, '{}'::jsonb) AS attributes
  , COALESCE(dg_names.group_names, '{}'::text[])::text[] AS group_names
  , COALESCE(ra_slugs.role_slugs, '{}'::text[])::text[] AS role_slugs
FROM organization_user_relationships our
INNER JOIN users u
  ON u.id = our.user_id
  AND u.deleted_at IS NULL
LEFT JOIN LATERAL (
  -- The member's directory profile, preferring an explicit user link over an
  -- email match so a stale email row cannot shadow the linked profile.
  SELECT d.id, d.attributes
  FROM directory_users d
  WHERE d.organization_id = our.organization_id
    AND d.deleted IS FALSE
    AND d.workos_deleted IS FALSE
    AND (d.user_id = u.id OR LOWER(d.email) = LOWER(u.email))
  ORDER BY (d.user_id = u.id) DESC, d.created_at
  LIMIT 1
) du ON TRUE
LEFT JOIN LATERAL (
  SELECT ARRAY_AGG(DISTINCT dg.name) AS group_names
  FROM directory_user_group_memberships m
  INNER JOIN directory_groups dg
    ON dg.id = m.directory_group_id
    AND dg.organization_id = our.organization_id
    AND dg.deleted IS FALSE
    AND dg.workos_deleted IS FALSE
  WHERE m.directory_user_id = du.id
    AND m.deleted IS FALSE
) dg_names ON TRUE
LEFT JOIN LATERAL (
  SELECT ARRAY_AGG(DISTINCT COALESCE(orr.workos_slug, gr.workos_slug))
    FILTER (WHERE COALESCE(orr.workos_slug, gr.workos_slug) IS NOT NULL) AS role_slugs
  FROM organization_role_assignments ra
  LEFT JOIN organization_roles orr
    ON ra.role_urn = 'role:organization:' || orr.id::text
    AND orr.organization_id = ra.organization_id
    AND orr.deleted IS FALSE
    AND orr.workos_deleted IS FALSE
  LEFT JOIN global_roles gr
    ON ra.role_urn = 'role:global:' || gr.id::text
    AND gr.deleted IS FALSE
    AND gr.workos_deleted IS FALSE
  WHERE ra.organization_id = our.organization_id
    AND (ra.user_id = u.id OR (u.workos_id IS NOT NULL AND ra.workos_user_id = u.workos_id))
    AND ra.deleted_at IS NULL
) ra_slugs ON TRUE
WHERE our.organization_id = @organization_id
  AND our.deleted IS FALSE
  AND u.email <> '';

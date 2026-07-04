-- name: CreateSpendRule :one
INSERT INTO spend_rules (
    organization_id
  , name
  , slug
  , description
  , target_expr
  , limit_usd
  , window_kind
  , warn_at_pct
  , action
  , enabled
)
VALUES (
    @organization_id
  , @name
  , @slug
  , @description
  , @target_expr
  , @limit_usd
  , @window_kind
  , @warn_at_pct
  , @action
  , @enabled
)
RETURNING *;

-- name: SpendRuleSlugExists :one
SELECT EXISTS (
  SELECT 1
  FROM spend_rules
  WHERE organization_id = @organization_id
    AND slug = @slug
    AND deleted IS FALSE
) AS taken;

-- name: GetSpendRule :one
SELECT *
FROM spend_rules
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- name: GetSpendRuleForUpdate :one
SELECT *
FROM spend_rules
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: ListSpendRules :many
SELECT *
FROM spend_rules
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListEnabledSpendRules :many
SELECT *
FROM spend_rules
WHERE organization_id = @organization_id
  AND enabled IS TRUE
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListOrganizationsWithEnabledSpendRules :many
SELECT DISTINCT organization_id
FROM spend_rules
WHERE enabled IS TRUE
  AND deleted IS FALSE;

-- name: UpdateSpendRule :one
UPDATE spend_rules
SET name = @name
  , description = @description
  , target_expr = @target_expr
  , limit_usd = @limit_usd
  , window_kind = @window_kind
  , warn_at_pct = @warn_at_pct
  , action = @action
  , enabled = @enabled
  , version = @version
  , evaluated_from = @evaluated_from
  , updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteSpendRule :one
UPDATE spend_rules
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: InsertSpendRuleVersion :exec
INSERT INTO spend_rule_versions (
    organization_id
  , spend_rule_id
  , version
  , target_expr
  , limit_usd
  , window_kind
  , warn_at_pct
  , action
)
VALUES (
    @organization_id
  , @spend_rule_id
  , @version
  , @target_expr
  , @limit_usd
  , @window_kind
  , @warn_at_pct
  , @action
)
ON CONFLICT (spend_rule_id, version) DO NOTHING;

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
ON CONFLICT (spend_rule_id, rule_urn, event_type, email, window_start) DO NOTHING;

-- name: ListSpendRuleEvents :many
SELECT ev.*, r.name AS rule_name
FROM spend_rule_events ev
INNER JOIN spend_rules r ON r.id = ev.spend_rule_id
WHERE ev.organization_id = @organization_id
  AND (sqlc.narg(spend_rule_id)::uuid IS NULL OR ev.spend_rule_id = sqlc.narg(spend_rule_id)::uuid)
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

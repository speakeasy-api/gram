-- name: InsertAuditLog :one
INSERT INTO audit_logs (
  organization_id,
  project_id,
  actor_id,
  actor_type,
  actor_display_name,
  actor_slug,
  action,
  subject_id,
  subject_type,
  subject_display_name,
  subject_slug,
  before_snapshot,
  after_snapshot,
  metadata,
  acting_surface,
  acting_client_id
) VALUES (
  @organization_id,
  @project_id,
  @actor_id,
  @actor_type,
  @actor_display_name,
  @actor_slug,
  @action,
  @subject_id,
  @subject_type,
  @subject_display_name,
  @subject_slug,
  @before_snapshot,
  @after_snapshot,
  @metadata,
  @acting_surface,
  @acting_client_id
)
RETURNING id, organization_id;

-- name: HasOpenRouterSpendCapAuditOperation :one
SELECT EXISTS (
  SELECT 1
  FROM audit_logs
  WHERE organization_id = @organization_id
    AND project_id IS NULL
    AND action = 'openrouter-key:set_spend_cap'
    AND subject_id = @subject_id
    AND metadata->>'operation_id' = @operation_id::text
) AS recorded;

-- name: GetLatestOpenRouterSpendCapAuditOperation :one
SELECT
  COALESCE(latest.operation_id, '')::text AS operation_id,
  COALESCE(latest.monthly_credits, 0)::bigint AS monthly_credits
FROM (VALUES (1)) AS singleton(value)
LEFT JOIN LATERAL (
  SELECT
    metadata->>'operation_id' AS operation_id,
    (after_snapshot->>'monthly_credits')::bigint AS monthly_credits
  FROM audit_logs
  WHERE organization_id = @organization_id
    AND project_id IS NULL
    AND action = 'openrouter-key:set_spend_cap'
    AND subject_id = @subject_id
  ORDER BY seq DESC
  LIMIT 1
) AS latest ON TRUE;

-- name: ListAuditLogs :many
-- When no subject_type filter is given, assistant activity events (one per
-- assistant tool call) are excluded so they don't drown out the platform
-- audit feed. The private admin caller can explicitly include all events.
-- The actor name is resolved at read time rather than trusted from the row.
-- Every writer stores the acting user's EMAIL in actor_display_name, so the
-- stored value renders the feed as a column of addresses; joining the
-- directory repairs the rows already written, and keeps names current when
-- someone is renamed. The stored value remains the fallback for actors with
-- no directory row: API keys and system actors. A soft-deleted user still
-- resolves, deliberately — they really did perform the action, and their name
-- is both more useful and less identifying than the email the row stored.
SELECT a.*, p.slug AS project_slug, u.display_name AS actor_user_display_name
FROM audit_logs a
LEFT JOIN projects p ON p.id = a.project_id
LEFT JOIN users u ON a.actor_type = 'user' AND u.id = a.actor_id
WHERE a.organization_id = @organization_id
  AND (
    sqlc.narg(project_id)::uuid IS NULL
    OR a.project_id = sqlc.narg(project_id)::uuid
  )
  AND (
    sqlc.narg(cursor_seq)::int8 IS NULL
    OR a.seq < sqlc.narg(cursor_seq)::int8
  )
  AND (
    sqlc.narg(actor_id)::text IS NULL
    OR a.actor_id = sqlc.narg(actor_id)::text
  )
  AND (
    sqlc.narg(action)::text IS NULL
    OR a.action = sqlc.narg(action)::text
  )
  AND (
    (
      sqlc.narg(subject_type)::text IS NULL
      AND (@include_assistant_events::boolean OR a.subject_type <> 'assistant')
    )
    OR a.subject_type = sqlc.narg(subject_type)::text
  )
  AND (
    sqlc.narg(subject_id)::text IS NULL
    OR a.subject_id = sqlc.narg(subject_id)::text
  )
  -- An empty or absent list is no filter, so a caller composing filters can
  -- always send the parameter.
  AND (
    coalesce(cardinality(sqlc.narg(subject_ids)::text[]), 0) = 0
    OR a.subject_id = ANY(sqlc.narg(subject_ids)::text[])
  )
  -- A row written before attribution existed has no surface. Coalescing here
  -- means filtering for 'unknown' finds those rows too, instead of returning
  -- nothing and implying the organization has no unattributed history.
  AND (
    sqlc.narg(acting_surface)::text IS NULL
    OR COALESCE(a.acting_surface, 'unknown') = sqlc.narg(acting_surface)::text
  )
  -- Half-open window, so consecutive ranges neither double-count a row nor
  -- drop one that lands exactly on a boundary.
  AND (
    sqlc.narg(created_from)::timestamptz IS NULL
    OR a.created_at >= sqlc.narg(created_from)::timestamptz
  )
  AND (
    sqlc.narg(created_to)::timestamptz IS NULL
    OR a.created_at < sqlc.narg(created_to)::timestamptz
  )
ORDER BY a.seq DESC
LIMIT 51;

-- name: ListAuditActorFacets :many
-- Assistant activity events are excluded: facets power the platform audit
-- feed, which hides them (see ListAuditLogs).
WITH filtered_logs AS (
  SELECT actor_id, actor_type, actor_display_name, acting_surface, seq
  FROM audit_logs
  WHERE organization_id = @organization_id
    AND subject_type <> 'assistant'
    AND (
      sqlc.narg(project_id)::uuid IS NULL
      OR project_id = sqlc.narg(project_id)::uuid
    )
), actor_counts AS (
  SELECT
    actor_id,
    COUNT(*)::bigint AS count,
    -- Flags actor ids that appear as user actors, so callers can restrict
    -- user-specific treatment (e.g. Speakeasy staff masking) to them.
    BOOL_OR(actor_type = 'user')::boolean AS is_user_actor,
    BOOL_OR(COALESCE(acting_surface = 'admin', FALSE))::boolean AS is_admin_actor
  FROM filtered_logs
  GROUP BY actor_id
), latest_actor_names AS (
  SELECT DISTINCT ON (actor_id)
    actor_id,
    actor_display_name
  FROM filtered_logs
  WHERE actor_display_name IS NOT NULL
    AND actor_display_name <> ''
  ORDER BY actor_id, seq DESC
)
SELECT
  actor_counts.actor_id AS value,
  -- Directory name first, for the same reason ListAuditLogs joins it: the
  -- stored value is the acting user's email, so the filter dropdown would
  -- otherwise list addresses while the feed beside it lists names.
  COALESCE(
    NULLIF(u.display_name, ''),
    latest_actor_names.actor_display_name,
    actor_counts.actor_id
  ) AS display_name,
  actor_counts.count,
  actor_counts.is_user_actor,
  actor_counts.is_admin_actor
FROM actor_counts
LEFT JOIN latest_actor_names ON latest_actor_names.actor_id = actor_counts.actor_id
LEFT JOIN users u ON actor_counts.is_user_actor AND u.id = actor_counts.actor_id
ORDER BY actor_counts.count DESC, actor_counts.actor_id ASC;

-- name: ListAuditSurfaceFacets :many
-- Assistant activity events are excluded: facets power the platform audit
-- feed, which hides them (see ListAuditLogs).
-- Rows predating attribution have no surface and are counted as 'unknown',
-- so the facet totals reconcile with the unfiltered feed rather than silently
-- omitting an organization's older history.
SELECT
  COALESCE(acting_surface, 'unknown')::text AS value,
  COALESCE(acting_surface, 'unknown')::text AS display_name,
  COUNT(*)::bigint AS count
FROM audit_logs
WHERE organization_id = @organization_id
  AND subject_type <> 'assistant'
  AND (
    sqlc.narg(project_id)::uuid IS NULL
    OR project_id = sqlc.narg(project_id)::uuid
  )
-- Group on the coalesced value, not the column: an organization can hold both
-- nulls from before attribution and rows the application wrote as 'unknown',
-- and grouping on the raw column would return two facets with the same label.
GROUP BY COALESCE(acting_surface, 'unknown')
ORDER BY count DESC, COALESCE(acting_surface, 'unknown') ASC;

-- name: ListAuditActionFacets :many
-- Assistant activity events are excluded: facets power the platform audit
-- feed, which hides them (see ListAuditLogs).
SELECT
  action AS value,
  action AS display_name,
  COUNT(*)::bigint AS count
FROM audit_logs
WHERE organization_id = @organization_id
  AND subject_type <> 'assistant'
  AND (
    sqlc.narg(project_id)::uuid IS NULL
    OR project_id = sqlc.narg(project_id)::uuid
  )
GROUP BY action
ORDER BY count DESC, action ASC;

-- name: UpdateAuditLogCreatedAtForTesting :exec
-- Test fixture for deterministic ordering and cursor-boundary coverage.
UPDATE audit_logs
SET created_at = @created_at
WHERE id = ANY(@ids::uuid[]);

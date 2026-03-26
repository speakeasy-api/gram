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
  metadata
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
  @metadata
)
RETURNING id;

-- name: ListAuditLogs :many
SELECT a.*, p.slug AS project_slug
FROM audit_logs a
LEFT JOIN projects p ON p.id = a.project_id
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
ORDER BY a.seq DESC
LIMIT 51;

-- name: ListAuditActorFacets :many
WITH filtered_logs AS (
  SELECT actor_id, actor_display_name, seq
  FROM audit_logs
  WHERE organization_id = @organization_id
    AND (
      sqlc.narg(project_id)::uuid IS NULL
      OR project_id = sqlc.narg(project_id)::uuid
    )
), actor_counts AS (
  SELECT actor_id, COUNT(*)::bigint AS count
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
  COALESCE(latest_actor_names.actor_display_name, actor_counts.actor_id) AS display_name,
  actor_counts.count
FROM actor_counts
LEFT JOIN latest_actor_names ON latest_actor_names.actor_id = actor_counts.actor_id
ORDER BY actor_counts.count DESC, actor_counts.actor_id ASC;

-- name: ListAuditActionFacets :many
SELECT
  action AS value,
  action AS display_name,
  COUNT(*)::bigint AS count
FROM audit_logs
WHERE organization_id = @organization_id
  AND (
    sqlc.narg(project_id)::uuid IS NULL
    OR project_id = sqlc.narg(project_id)::uuid
  )
GROUP BY action
ORDER BY count DESC, action ASC;

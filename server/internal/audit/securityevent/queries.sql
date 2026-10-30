-- Queries for security_audit_logs, the log of security activity that is not a
-- configuration decision. See the securityevent package.

-- name: InsertSecurityAuditLog :one
INSERT INTO security_audit_logs (
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

-- name: ListSecurityAuditLogs :many
-- Mirrors ListAuditLogs in audit/queries.sql over security_audit_logs, with
-- the same parameters and columns, so callers convert params and rows between
-- the two generated types and share one reader. Keep the two in step.
SELECT a.*, p.slug AS project_slug, u.display_name AS actor_user_display_name
FROM security_audit_logs a
LEFT JOIN projects p ON p.id = a.project_id
LEFT JOIN organization_user_relationships actor_membership
  ON a.actor_type = 'user'
  AND actor_membership.organization_id = a.organization_id
  AND actor_membership.user_id = a.actor_id
LEFT JOIN users u ON u.id = actor_membership.user_id
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
  AND (
    coalesce(cardinality(sqlc.narg(subject_ids)::text[]), 0) = 0
    OR a.subject_id = ANY(sqlc.narg(subject_ids)::text[])
  )
  AND (
    sqlc.narg(acting_surface)::text IS NULL
    OR COALESCE(a.acting_surface, 'unknown') = sqlc.narg(acting_surface)::text
  )
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

-- name: ListSecurityActorFacets :many
-- Mirrors ListAuditActorFacets in audit/queries.sql over security_audit_logs; keep
-- the parameters and columns identical so the generated types convert.
WITH filtered_logs AS (
  SELECT actor_id, actor_type, actor_display_name, acting_surface, seq
  FROM security_audit_logs
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
    BOOL_OR(actor_type = 'user')::boolean AS is_user_actor,
    BOOL_OR(COALESCE(acting_surface IN ('admin', 'admin_mcp'), FALSE))::boolean AS is_admin_actor
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
LEFT JOIN organization_user_relationships actor_membership
  ON actor_counts.is_user_actor
  AND actor_membership.organization_id = @organization_id
  AND actor_membership.user_id = actor_counts.actor_id
LEFT JOIN users u ON u.id = actor_membership.user_id
ORDER BY actor_counts.count DESC, actor_counts.actor_id ASC;

-- name: ListSecuritySurfaceFacets :many
-- Mirrors ListAuditSurfaceFacets in audit/queries.sql over security_audit_logs; keep
-- the parameters and columns identical so the generated types convert.
SELECT
  COALESCE(acting_surface, 'unknown')::text AS value,
  COALESCE(acting_surface, 'unknown')::text AS display_name,
  COUNT(*)::bigint AS count
FROM security_audit_logs
WHERE organization_id = @organization_id
  AND subject_type <> 'assistant'
  AND (
    sqlc.narg(project_id)::uuid IS NULL
    OR project_id = sqlc.narg(project_id)::uuid
  )
GROUP BY COALESCE(acting_surface, 'unknown')
ORDER BY count DESC, COALESCE(acting_surface, 'unknown') ASC;

-- name: ListSecurityActionFacets :many
-- Mirrors ListAuditActionFacets in audit/queries.sql over security_audit_logs; keep
-- the parameters and columns identical so the generated types convert.
SELECT
  action AS value,
  action AS display_name,
  COUNT(*)::bigint AS count
FROM security_audit_logs
WHERE organization_id = @organization_id
  AND subject_type <> 'assistant'
  AND (
    sqlc.narg(project_id)::uuid IS NULL
    OR project_id = sqlc.narg(project_id)::uuid
  )
GROUP BY action
ORDER BY count DESC, action ASC;

-- name: ListRuntimeAdmittedSubjects :many
-- Returns which of the given workload admissions a Kit runtime admitted for
-- itself. The audit trail is the signal because no customer can write it: an
-- admission's tags and name are customer-editable. Admissions recorded before
-- security_audit_logs existed sit in audit_logs under the system:kit-runtime
-- actor until the audit-kit-runtime-security backfill moves them.
SELECT s.subject_id
FROM security_audit_logs s
WHERE s.organization_id = @organization_id
  AND s.subject_type = 'workload_admission'
  AND s.action = 'workload-admission:admit'
  AND s.subject_id = ANY(@subject_ids::text[])
UNION
SELECT a.subject_id
FROM audit_logs a
WHERE a.organization_id = @organization_id
  AND a.subject_type = 'workload_admission'
  AND a.action = 'workload-admission:admit'
  AND a.actor_type = 'system'
  AND a.actor_id = 'kit-runtime'
  AND a.subject_id = ANY(@subject_ids::text[]);

-- name: CountKitRuntimeAuditLogs :one
-- Counts the audit_logs entries MoveKitRuntimeAuditLogs would move.
SELECT COUNT(*)::bigint
FROM audit_logs
WHERE subject_type = 'workload_admission'
  AND action IN ('workload-admission:admit', 'workload-admission:withdraw')
  AND actor_type = 'system'
  AND actor_id = 'kit-runtime';

-- name: MoveKitRuntimeAuditLogs :execrows
-- Moves the admit and withdraw entries a Kit runtime wrote for its own
-- workload admissions from audit_logs to security_audit_logs. Withdrawals a
-- person made stay: older entries cannot tell an issuer withdrawal's cascade
-- from a direct withdrawal, and a direct one is a configuration change.
-- Entries keep every column, including seq, which sits below the security
-- log's identity range so moved entries sort before the ones written there
-- directly, in their original order. Moves at most batch_size rows; callers
-- repeat until no rows move.
WITH batch AS (
  SELECT id
  FROM audit_logs
  WHERE subject_type = 'workload_admission'
    AND action IN ('workload-admission:admit', 'workload-admission:withdraw')
    AND actor_type = 'system'
    AND actor_id = 'kit-runtime'
  ORDER BY seq
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED
), moved AS (
  DELETE FROM audit_logs
  USING batch
  WHERE audit_logs.id = batch.id
  RETURNING audit_logs.*
)
INSERT INTO security_audit_logs (
  id,
  seq,
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
  acting_client_id,
  created_at
)
SELECT
  id,
  seq,
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
  acting_client_id,
  created_at
FROM moved;

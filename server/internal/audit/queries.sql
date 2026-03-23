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

-- name: GetLatestAuditLogByAction :one
SELECT
  action,
  subject_type,
  subject_display_name,
  subject_slug,
  metadata,
  before_snapshot,
  after_snapshot
FROM audit_logs
WHERE action = @action
ORDER BY seq DESC
LIMIT 1;

-- name: ListProjectAuditLogs :many
SELECT *
FROM audit_logs
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND (
    sqlc.narg(cursor_seq)::int8 IS NULL
    OR seq < sqlc.narg(cursor_seq)::int8
  )
ORDER BY seq DESC
LIMIT 51;

-- name: CountAuditLogs :one
SELECT COUNT(*)
FROM audit_logs;

-- name: CountAuditLogsByAction :one
SELECT COUNT(*)
FROM audit_logs
WHERE action = @action;

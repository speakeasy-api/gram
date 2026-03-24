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
ORDER BY a.seq DESC
LIMIT 51;

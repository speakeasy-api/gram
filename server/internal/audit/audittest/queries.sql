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

-- name: CountAuditLogs :one
SELECT COUNT(*)
FROM audit_logs;

-- name: CountAuditLogsByAction :one
SELECT COUNT(*)
FROM audit_logs
WHERE action = @action;

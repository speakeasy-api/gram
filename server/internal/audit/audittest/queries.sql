-- name: GetLatestAuditLogByAction :one
SELECT
  action,
  project_id,
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

-- name: GetLatestOutboxPayloadByOrg :one
-- Returns the JSON payload of the most-recently inserted outbox entry for an org+event_type pair.
SELECT payload
FROM outbox
WHERE organization_id = @organization_id
  AND event_type = @event_type
ORDER BY id DESC
LIMIT 1;

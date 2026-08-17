-- name: GetLatestAuditLogByAction :one
SELECT
  action,
  organization_id,
  project_id,
  actor_id,
  actor_type,
  actor_display_name,
  subject_id,
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
-- Returns the marshaled webhook envelope of the most-recently enqueued outbox
-- entry for an org+event_type pair. Callers decode it to reach the JSON payload.
-- The event type is matched on the Pub/Sub message attribute because the outbox
-- row no longer carries webhook-specific columns.
SELECT message
FROM publish_outbox
WHERE organization_id = @organization_id
  AND attributes->>'event_type' = @event_type::text
ORDER BY id DESC
LIMIT 1;

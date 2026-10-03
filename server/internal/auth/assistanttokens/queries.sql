-- name: GetAssistantTokenRevocation :one
SELECT t.deleted AS thread_deleted, a.deleted AS assistant_deleted, a.status AS assistant_status
FROM assistant_threads t
JOIN assistants a ON a.id = t.assistant_id
WHERE t.id = @thread_id
  AND t.assistant_id = @assistant_id
  AND t.project_id = @project_id
  AND a.project_id = @project_id;

-- name: GetAssistantRevocation :one
SELECT a.deleted AS assistant_deleted, a.status AS assistant_status
FROM assistants a
WHERE a.id = @assistant_id
  AND a.project_id = @project_id;

-- name: GetMCPAuthExecutionOrigin :one
SELECT e.event_id, e.normalized_payload_json
FROM assistant_thread_events e
JOIN assistant_threads t ON t.id = e.assistant_thread_id AND t.project_id = e.project_id
JOIN assistants a ON a.id = e.assistant_id AND a.project_id = e.project_id
WHERE e.id = @originating_event_id
  AND e.project_id = @project_id AND e.assistant_id = @assistant_id
  AND e.assistant_thread_id = @thread_id AND t.assistant_id = @assistant_id
  AND a.organization_id = @organization_id
  AND e.deleted IS FALSE AND t.deleted IS FALSE AND a.deleted IS FALSE;

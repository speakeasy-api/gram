-- name: GetAssistantTokenRevocation :one
SELECT t.deleted AS thread_deleted, a.deleted AS assistant_deleted, a.status AS assistant_status
FROM assistant_threads t
JOIN assistants a ON a.id = t.assistant_id
WHERE t.id = @thread_id
  AND t.assistant_id = @assistant_id;

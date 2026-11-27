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

-- name: ListKitInferenceSessionBindings :many
-- Resolve provider session attribution only inside the authenticated runtime.
SELECT s.acp_session_id, t.id AS thread_id, t.chat_id
FROM assistant_kit_sessions s
JOIN assistant_runtimes r ON r.id = s.runtime_id AND r.project_id = s.project_id AND r.assistant_id = s.assistant_id
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
JOIN assistant_threads t ON t.id = s.assistant_thread_id AND t.project_id = s.project_id AND t.assistant_id = s.assistant_id
JOIN chats c ON c.id = t.chat_id AND c.project_id = t.project_id
WHERE s.runtime_id = @runtime_id AND s.project_id = @project_id AND s.assistant_id = @assistant_id
  AND r.runtime_version = 3 AND a.runtime_version = 3
  AND NOT r.deleted AND NOT r.ended AND r.state IN ('starting', 'active') AND NOT a.deleted AND a.status = 'active'
  AND NOT t.deleted AND NOT c.deleted
  AND (
    r.backend_metadata_json->'workload' = @workload_binding::jsonb
    OR EXISTS (
      SELECT 1
      FROM assistant_ate_actors act
      WHERE act.runtime_id = r.id
        AND act.project_id = r.project_id
        AND act.assistant_id = r.assistant_id
        AND act.assistant_thread_id = s.assistant_thread_id
        AND NOT act.deleted
        AND act.workload_binding_json = @workload_binding::jsonb
    )
  );

-- name: ListKitWorkloadBindings :many
-- The workload subject is unique to one runtime generation (or ATE thread actor), not a guest claim.
SELECT r.id AS runtime_id, r.assistant_id, r.backend_metadata_json,
  (r.runtime_version = 3 AND a.runtime_version = 3
   AND NOT r.deleted AND NOT r.ended AND r.state IN ('starting', 'active')
   AND NOT a.deleted AND a.status = 'active')::boolean AS active
FROM assistant_runtimes r
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
WHERE r.project_id = @project_id AND a.organization_id = @organization_id
  AND r.backend_metadata_json->'workload'->>'issuer_id' = @issuer_id::text
  AND r.backend_metadata_json->'workload'->>'subject' = @subject::text
UNION ALL
SELECT r.id AS runtime_id, r.assistant_id,
  jsonb_build_object(
    'runtime_id', r.id,
    'assistant_id', r.assistant_id,
    'project_id', r.project_id,
    'runtime_version', r.runtime_version,
    'workload', act.workload_binding_json
  ) AS backend_metadata_json,
  (r.runtime_version = 3 AND a.runtime_version = 3
   AND NOT r.deleted AND NOT r.ended AND r.state IN ('starting', 'active')
   AND NOT a.deleted AND a.status = 'active'
   AND NOT act.deleted)::boolean AS active
FROM assistant_ate_actors act
JOIN assistant_runtimes r ON r.id = act.runtime_id AND r.project_id = act.project_id
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
WHERE act.project_id = @project_id AND act.organization_id = @organization_id
  AND act.workload_binding_json->>'issuer_id' = @issuer_id::text
  AND act.workload_binding_json->>'subject' = @subject::text;

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

-- name: IssueInferenceCredential :one
INSERT INTO assistant_inference_credentials (project_id, assistant_id, runtime_id, token_hash, identity_json, policy_json, policy_version, expires_at)
SELECT @project_id, @assistant_id, r.id, @token_hash, @identity_json, @policy_json, @policy_version, @expires_at
FROM assistant_runtimes r JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
WHERE r.id = @runtime_id AND r.project_id = @project_id AND r.assistant_id = @assistant_id
AND NOT r.deleted AND NOT r.ended AND NOT a.deleted AND a.status = 'active'
RETURNING id;

-- name: ReadInferenceCredential :one
SELECT c.* FROM assistant_inference_credentials c
JOIN assistant_runtimes r ON r.id = c.runtime_id AND r.project_id = c.project_id AND r.assistant_id = c.assistant_id
JOIN assistants a ON a.id = c.assistant_id AND a.project_id = c.project_id
WHERE c.token_hash = @token_hash AND c.revoked_at IS NULL AND c.expires_at > clock_timestamp()
AND NOT r.deleted AND NOT r.ended AND NOT a.deleted AND a.status = 'active';

-- name: RevokeInferenceCredential :exec
UPDATE assistant_inference_credentials SET revoked_at = clock_timestamp()
WHERE project_id = @project_id AND id = @id AND revoked_at IS NULL;

-- name: InferenceChatThread :one
SELECT t.id FROM assistant_threads t JOIN chats c ON c.id = t.chat_id AND c.project_id = t.project_id
WHERE t.project_id = @project_id AND t.assistant_id = @assistant_id AND t.chat_id = @chat_id AND NOT t.deleted AND NOT c.deleted;

-- name: RevokeRuntimeInferenceCredentials :exec
UPDATE assistant_inference_credentials SET revoked_at = clock_timestamp()
WHERE project_id = @project_id AND runtime_id = @runtime_id AND revoked_at IS NULL;

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

-- name: ReadKitAPIKeyBinding :one
SELECT c.* FROM assistant_inference_credentials c
JOIN api_keys k ON k.id = c.api_key_id AND k.project_id = c.project_id
JOIN assistant_runtimes r ON r.id = c.runtime_id AND r.project_id = c.project_id AND r.assistant_id = c.assistant_id
JOIN assistants a ON a.id = c.assistant_id AND a.project_id = c.project_id
WHERE c.api_key_id = @api_key_id AND c.revoked_at IS NULL AND NOT k.deleted AND (k.expires_at IS NULL OR k.expires_at > clock_timestamp())
AND NOT r.deleted AND NOT r.ended AND r.state IN ('starting', 'active') AND NOT a.deleted AND a.status = 'active';

-- name: RevokeInferenceCredential :exec
WITH revoked AS (
  UPDATE assistant_inference_credentials c SET revoked_at = clock_timestamp()
  WHERE c.project_id = @project_id AND c.api_key_id = @api_key_id AND c.revoked_at IS NULL RETURNING c.api_key_id
)
UPDATE api_keys SET deleted_at = clock_timestamp() WHERE id IN (SELECT api_key_id FROM revoked) AND NOT deleted;

-- name: RevokeRuntimeInferenceCredentials :exec
WITH revoked AS (
  UPDATE assistant_inference_credentials c SET revoked_at = clock_timestamp()
  WHERE c.project_id = @project_id AND c.runtime_id = @runtime_id AND c.revoked_at IS NULL RETURNING c.api_key_id
)
UPDATE api_keys SET deleted_at = clock_timestamp() WHERE id IN (SELECT api_key_id FROM revoked) AND NOT deleted;

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
  AND NOT t.deleted AND NOT c.deleted;

-- name: IssueKitAPIKeyBinding :one
-- The api_key_id references the already-created native API key; this row holds only
-- control-plane runtime authority and does not define a new wire credential.
INSERT INTO assistant_inference_credentials (api_key_id, project_id, assistant_id, runtime_id, identity_json)
SELECT @api_key_id, @project_id, @assistant_id, r.id, @identity_json
FROM assistant_runtimes r JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
WHERE r.id = @runtime_id AND r.project_id = @project_id AND r.assistant_id = @assistant_id
AND r.runtime_version = 3 AND a.runtime_version = 3
AND NOT r.deleted AND NOT r.ended AND r.state IN ('starting', 'active') AND NOT a.deleted AND a.status = 'active'
RETURNING id;

-- name: IsManagedKitAPIKey :one
SELECT EXISTS (SELECT 1 FROM assistant_inference_credentials WHERE api_key_id = @api_key_id);

-- name: CreateKitAPIKey :one
INSERT INTO api_keys (
    organization_id
  , project_id
  , created_by_user_id
  , name
  , key_prefix
  , key_hash
  , scopes
  , subject_urn
  , delegated_grants
  , delegated_grants_version
  , expires_at
) VALUES (
    @organization_id
  , @project_id
  , @created_by_user_id
  , @name
  , @key_prefix
  , @key_hash
  , ARRAY[]::text[]
  , @subject_urn
  , @delegated_grants::jsonb
  , @delegated_grants_version
  , @expires_at
)
RETURNING *;

-- name: CreateSession :one
INSERT INTO chat_sessions (
  project_id,
  organization_id,
  external_user_id,
  embed_origin
) VALUES (
  @project_id,
  @organization_id,
  @external_user_id,
  @embed_origin
)
RETURNING *;

-- name: GetSession :one
SELECT *
FROM chat_sessions
WHERE id = @id
  AND deleted IS FALSE;

-- name: GetSessionByProjectAndExternalUser :one
SELECT *
FROM chat_sessions
WHERE project_id = @project_id
  AND external_user_id = @external_user_id
  AND deleted IS FALSE
ORDER BY created_at DESC
LIMIT 1;

-- name: UpsertCredential :one
INSERT INTO chat_session_credentials (
  chat_session_id,
  project_id,
  toolset_id,
  access_token_encrypted,
  refresh_token_encrypted,
  token_type,
  scope,
  expires_at
) VALUES (
  @chat_session_id,
  @project_id,
  @toolset_id,
  @access_token_encrypted,
  @refresh_token_encrypted,
  @token_type,
  @scope,
  @expires_at
)
ON CONFLICT (chat_session_id, toolset_id)
DO UPDATE SET
  access_token_encrypted = EXCLUDED.access_token_encrypted,
  refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
  token_type = EXCLUDED.token_type,
  scope = EXCLUDED.scope,
  expires_at = EXCLUDED.expires_at,
  updated_at = clock_timestamp()
RETURNING *;

-- name: GetCredentialByToolset :one
SELECT *
FROM chat_session_credentials
WHERE chat_session_id = @chat_session_id
  AND toolset_id = @toolset_id;

-- name: DeleteCredential :exec
DELETE FROM chat_session_credentials
WHERE chat_session_id = @chat_session_id
  AND toolset_id = @toolset_id;

-- name: ListCredentialsBySession :many
SELECT *
FROM chat_session_credentials
WHERE chat_session_id = @chat_session_id;

-- name: GetKeyForResolution :one
SELECT organization_id, api_key_encrypted, api_key_org_encrypted
FROM model_provider_keys
WHERE project_id = @project_id
  AND slot = ANY (@slots::text[])
  AND provider = @provider
  AND enabled
  AND deleted IS FALSE
ORDER BY (slot = @preferred_slot::text) DESC
LIMIT 1;

-- name: ListKeysByProject :many
SELECT *
FROM model_provider_keys
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY slot;

-- name: GetKeyByIDForUpdate :one
SELECT *
FROM model_provider_keys
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: InsertKey :one
-- The key is stored by SetKeyAPIKey in the same transaction.
INSERT INTO model_provider_keys (
    organization_id
  , project_id
  , slot
  , provider
  , enabled
) VALUES (
    @organization_id
  , @project_id
  , @slot
  , @provider
  , @enabled
)
RETURNING *;

-- name: SetKeyAPIKey :one
-- Stores both copies of the key sealed under the decided transition
-- generation. No rows means the key is gone or the transition moved;
-- aiproviderkeys tells the two apart.
UPDATE model_provider_keys AS k
SET api_key_encrypted = sqlc.narg('platform_copy'),
    api_key_org_encrypted = sqlc.narg('org_copy')
WHERE k.id = @id
  AND k.organization_id = @organization_id
  AND k.deleted IS FALSE
  AND COALESCE((
    SELECT t.generation FROM organization_encryption_transitions AS t
    WHERE t.organization_id = k.organization_id AND t.use_case = 'ai-provider-keys'
    FOR KEY SHARE
  ), 0) = @decided_generation
RETURNING *;

-- name: SoftDeleteKeyBySlot :many
UPDATE model_provider_keys
SET deleted_at = clock_timestamp()
WHERE project_id = @project_id
  AND slot = @slot
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteKeyByID :one
UPDATE model_provider_keys
SET deleted_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: SetKeyEnabled :one
UPDATE model_provider_keys
SET enabled = @enabled
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: GetKeyForResolution :one
SELECT id, api_key_encrypted, api_key_org_encrypted
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
INSERT INTO model_provider_keys (
    organization_id
  , project_id
  , slot
  , provider
  , api_key_encrypted
  , api_key_org_encrypted
  , enabled
) VALUES (
    @organization_id
  , @project_id
  , @slot
  , @provider
  , @api_key_encrypted
  , @api_key_org_encrypted
  , @enabled
)
RETURNING *;

-- name: BackfillKeyOrgEncrypted :exec
-- Stores the organization-encrypted copy of a key read before it had one. The
-- application-key ciphertext must still be the one that was read, so a key
-- replaced in the meantime is left alone.
UPDATE model_provider_keys
SET api_key_org_encrypted = @api_key_org_encrypted
WHERE id = @id
  AND project_id = @project_id
  AND api_key_encrypted = @api_key_encrypted
  AND api_key_org_encrypted IS NULL;

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

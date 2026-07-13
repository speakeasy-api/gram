-- name: GetKeyForResolution :one
SELECT api_key_encrypted
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

-- name: InsertKey :one
INSERT INTO model_provider_keys (
    organization_id
  , project_id
  , slot
  , provider
  , api_key_encrypted
  , enabled
) VALUES (
    @organization_id
  , @project_id
  , @slot
  , @provider
  , @api_key_encrypted
  , @enabled
)
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

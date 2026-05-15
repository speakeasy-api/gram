-- name: GetConfigByProject :one
SELECT *
FROM cursor_integration_configs
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: UpsertConfig :one
INSERT INTO cursor_integration_configs (
    organization_id
  , project_id
  , api_key_encrypted
  , enabled
) VALUES (
    @organization_id
  , @project_id
  , @api_key_encrypted
  , @enabled
)
ON CONFLICT (organization_id, project_id) WHERE deleted IS FALSE
DO UPDATE SET
    api_key_encrypted = EXCLUDED.api_key_encrypted
  , enabled = EXCLUDED.enabled
  , updated_at = clock_timestamp()
RETURNING *;

-- name: SoftDeleteConfig :exec
UPDATE cursor_integration_configs
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListEnabledConfigs :many
SELECT *
FROM cursor_integration_configs
WHERE enabled IS TRUE
  AND deleted IS FALSE
  AND api_key_encrypted IS NOT NULL
ORDER BY organization_id, project_id;

-- name: UpdateLastPolledAt :exec
UPDATE cursor_integration_configs
SET last_polled_at = @last_polled_at,
    updated_at = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE;

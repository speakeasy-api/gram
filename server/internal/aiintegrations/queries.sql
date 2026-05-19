-- name: GetConfigByOrgAndProvider :one
SELECT
    c.*
  , s.id AS sync_id
  , s.last_polled_at
  , s.created_at AS sync_created_at
  , s.updated_at AS sync_updated_at
FROM ai_integration_configs c
JOIN ai_integration_syncs s ON s.ai_integration_config_id = c.id
WHERE c.organization_id = @organization_id
  AND c.provider = @provider
  AND c.deleted IS FALSE;

-- name: GetFirstProjectByOrganization :one
SELECT id
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at ASC, id ASC
LIMIT 1;

-- name: UpsertConfig :one
INSERT INTO ai_integration_configs (
    organization_id
  , provider
  , project_id
  , api_key_encrypted
  , enabled
) VALUES (
    @organization_id
  , @provider
  , @project_id
  , @api_key_encrypted
  , @enabled
)
ON CONFLICT (organization_id, provider) WHERE deleted IS FALSE
DO UPDATE SET
    project_id = EXCLUDED.project_id
  , api_key_encrypted = EXCLUDED.api_key_encrypted
  , enabled = EXCLUDED.enabled
  , updated_at = clock_timestamp()
RETURNING *;

-- name: EnsureSync :one
WITH inserted AS (
  INSERT INTO ai_integration_syncs (
    ai_integration_config_id
  ) VALUES (
    @ai_integration_config_id
  )
  ON CONFLICT (ai_integration_config_id) DO NOTHING
  RETURNING *
)
SELECT *
FROM inserted
UNION ALL
SELECT *
FROM ai_integration_syncs
WHERE ai_integration_config_id = @ai_integration_config_id
LIMIT 1;

-- name: SoftDeleteConfig :exec
UPDATE ai_integration_configs
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE;

-- name: ListEnabledConfigsByProvider :many
SELECT
    c.*
  , s.id AS sync_id
  , s.last_polled_at
  , s.created_at AS sync_created_at
  , s.updated_at AS sync_updated_at
FROM ai_integration_configs c
JOIN ai_integration_syncs s ON s.ai_integration_config_id = c.id
WHERE c.provider = @provider
  AND c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND c.api_key_encrypted IS NOT NULL
ORDER BY c.organization_id, c.provider;

-- name: UpdateSyncLastPolledAt :exec
UPDATE ai_integration_syncs
SET last_polled_at = @last_polled_at,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id;

-- name: ListUsagePollCandidates :many
SELECT
    c.*
  , s.id AS sync_id
  , s.last_polled_at
  , s.created_at AS sync_created_at
  , s.updated_at AS sync_updated_at
FROM ai_integration_syncs s
JOIN ai_integration_configs c ON c.id = s.ai_integration_config_id
WHERE c.provider = @provider
  AND c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND c.api_key_encrypted IS NOT NULL
  AND s.last_polled_at < @last_polled_before
  AND (
    sqlc.narg('cursor_last_polled_at')::timestamptz IS NULL
    OR (s.last_polled_at, c.organization_id, c.provider) > (
      sqlc.narg('cursor_last_polled_at')::timestamptz,
      sqlc.narg('cursor_organization_id')::text,
      sqlc.narg('cursor_provider')::text
    )
  )
ORDER BY s.last_polled_at ASC, c.organization_id ASC, c.provider ASC
LIMIT @limit_count;

-- name: UpdateUsagePollWatermark :exec
UPDATE ai_integration_syncs
SET last_polled_at = @last_polled_at,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id;

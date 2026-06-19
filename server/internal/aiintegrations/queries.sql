-- name: GetConfigByOrgAndProvider :one
SELECT
    c.*
  , s.id AS sync_id
  , s.poll_watermark_at
  , s.next_poll_after
  , s.last_poll_error
  , s.last_poll_failed_at
  , s.last_poll_success_at
  , s.consecutive_failures
  , s.last_cursor_id
  , s.created_at AS sync_created_at
  , s.updated_at AS sync_updated_at
FROM ai_integration_configs c
JOIN ai_integration_syncs s ON s.ai_integration_config_id = c.id
WHERE c.organization_id = @organization_id
  AND c.provider = @provider
  AND c.deleted IS FALSE;

-- name: CountConfigsByOrganization :one
SELECT count(*)
FROM ai_integration_configs
WHERE organization_id = @organization_id
  AND (@include_deleted::bool OR deleted IS FALSE);

-- name: GetFirstProjectByOrganization :one
SELECT id
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at ASC, id ASC
LIMIT 1;

-- name: InsertConfig :one
INSERT INTO ai_integration_configs (
    organization_id
  , provider
  , project_id
  , external_organization_id
  , api_key_encrypted
  , enabled
) VALUES (
    @organization_id
  , @provider
  , @project_id
  , @external_organization_id
  , @api_key_encrypted
  , @enabled
)
RETURNING *;

-- name: UpdateConfigSettings :one
UPDATE ai_integration_configs
SET project_id = @project_id,
    external_organization_id = @external_organization_id,
    enabled = @enabled,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
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
  , s.poll_watermark_at
  , s.next_poll_after
  , s.last_poll_error
  , s.last_poll_failed_at
  , s.last_poll_success_at
  , s.consecutive_failures
  , s.last_cursor_id
  , s.created_at AS sync_created_at
  , s.updated_at AS sync_updated_at
FROM ai_integration_configs c
JOIN ai_integration_syncs s ON s.ai_integration_config_id = c.id
WHERE c.provider = @provider
  AND c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND c.api_key_encrypted IS NOT NULL
ORDER BY c.organization_id, c.provider;

-- name: ResetUsagePollState :exec
UPDATE ai_integration_syncs
SET poll_watermark_at = @poll_watermark_at,
    next_poll_after = @next_poll_after,
    last_poll_error = NULL,
    last_poll_failed_at = NULL,
    last_poll_success_at = NULL,
    consecutive_failures = 0,
    last_cursor_id = NULL,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id;

-- name: ListUsagePollCandidates :many
SELECT
    c.id
  , c.organization_id
  , om.slug AS organization_slug
  , c.provider
FROM ai_integration_syncs s
JOIN ai_integration_configs c ON c.id = s.ai_integration_config_id
JOIN organization_metadata om ON om.id = c.organization_id
WHERE c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND c.api_key_encrypted IS NOT NULL
  AND s.next_poll_after <= @poll_due_before
ORDER BY s.next_poll_after ASC, c.organization_id ASC, c.provider ASC
LIMIT @limit_count;

-- name: GetUsagePollConfigByID :one
SELECT
    c.*
  , s.id AS sync_id
  , s.poll_watermark_at
  , s.next_poll_after
  , s.last_poll_error
  , s.last_poll_failed_at
  , s.last_poll_success_at
  , s.consecutive_failures
  , s.last_cursor_id
  , s.created_at AS sync_created_at
  , s.updated_at AS sync_updated_at
FROM ai_integration_configs c
JOIN ai_integration_syncs s ON s.ai_integration_config_id = c.id
WHERE c.id = @ai_integration_config_id
  AND c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND c.api_key_encrypted IS NOT NULL;

-- name: RecordUsagePollSuccess :exec
UPDATE ai_integration_syncs
SET poll_watermark_at = @poll_watermark_at,
    next_poll_after = @next_poll_after,
    last_poll_error = NULL,
    last_poll_failed_at = NULL,
    last_poll_success_at = clock_timestamp(),
    consecutive_failures = 0,
    last_cursor_id = @last_cursor_id,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id;

-- name: RecordUsagePollFailure :exec
UPDATE ai_integration_syncs
SET next_poll_after = @next_poll_after,
    last_poll_error = @last_poll_error,
    last_poll_failed_at = clock_timestamp(),
    consecutive_failures = consecutive_failures + 1,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id;

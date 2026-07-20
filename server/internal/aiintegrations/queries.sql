-- One sync schedule per provider shares its name with the config's provider,
-- so config-level reads join on s.schedule = c.provider. The other schedules
-- (e.g. anthropic_analytics) are read by their own queries.
--
-- schedule/kind are nullable during the expand-contract transition: rows
-- written before the columns existed carry NULL until a worker adopts them
-- (see AdoptLegacySyncSchedulesForProvider). Config-level reads treat a NULL
-- schedule as the provider-named schedule so those rows stay visible until
-- adoption; the fallbacks go away with the contract migration.

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
JOIN ai_integration_syncs s
  ON s.ai_integration_config_id = c.id
 AND (s.schedule = c.provider OR s.schedule IS NULL)
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
  , billing_mode
) VALUES (
    @organization_id
  , @provider
  , @project_id
  , @external_organization_id
  , @api_key_encrypted
  , @enabled
  , @billing_mode
)
RETURNING *;

-- name: UpdateConfigSettings :one
UPDATE ai_integration_configs
SET project_id = @project_id,
    external_organization_id = @external_organization_id,
    enabled = @enabled,
    billing_mode = @billing_mode,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
RETURNING *;

-- name: EnsureSync :one
WITH inserted AS (
  INSERT INTO ai_integration_syncs (
      ai_integration_config_id
    , schedule
    , kind
    , poll_watermark_at
    , next_poll_after
  ) VALUES (
      @ai_integration_config_id
    , @schedule
    , @kind
    , @poll_watermark_at
    , @next_poll_after
  )
  ON CONFLICT (ai_integration_config_id, schedule) DO UPDATE SET updated_at = ai_integration_syncs.updated_at
  RETURNING *
)
SELECT *
FROM inserted
UNION ALL
SELECT *
FROM ai_integration_syncs
WHERE ai_integration_config_id = @ai_integration_config_id
  AND schedule = @schedule
LIMIT 1;

-- AdoptLegacySyncSchedule labels one config's pre-schedule sync row as the
-- provider-named schedule so schedule-keyed reads and upserts see it. The
-- NOT EXISTS guard keeps the update from tripping the unique index if a
-- labeled provider-named row already exists next to an unlabeled legacy row.
-- name: AdoptLegacySyncSchedule :exec
UPDATE ai_integration_syncs s
SET schedule = @schedule,
    kind = @kind,
    updated_at = clock_timestamp()
WHERE s.ai_integration_config_id = @ai_integration_config_id
  AND s.schedule IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM ai_integration_syncs other
    WHERE other.ai_integration_config_id = s.ai_integration_config_id
      AND other.schedule = @schedule
  );

-- AdoptLegacySyncSchedulesForProvider is the bulk variant run ahead of
-- candidate listing. It only touches active configs: enabled, not deleted,
-- with an API key. Rows of inactive configs are deliberately left alone.
-- name: AdoptLegacySyncSchedulesForProvider :exec
UPDATE ai_integration_syncs s
SET schedule = c.provider,
    kind = @kind,
    updated_at = clock_timestamp()
FROM ai_integration_configs c
WHERE c.id = s.ai_integration_config_id
  AND c.provider = @provider
  AND c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND c.api_key_encrypted IS NOT NULL
  AND s.schedule IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM ai_integration_syncs other
    WHERE other.ai_integration_config_id = s.ai_integration_config_id
      AND other.schedule = c.provider
  );

-- EnsureProviderSyncSchedules inserts one schedule's sync row for every
-- active config of a provider that is missing it, due immediately with the
-- caller's initial watermark (epoch for time-kind schedules, now for
-- cursor-kind ones). Existing rows are untouched.
-- name: EnsureProviderSyncSchedules :exec
INSERT INTO ai_integration_syncs (
    ai_integration_config_id
  , schedule
  , kind
  , poll_watermark_at
  , next_poll_after
)
SELECT c.id, @schedule, @kind, @poll_watermark_at, @next_poll_after
FROM ai_integration_configs c
WHERE c.provider = @provider
  AND c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND c.api_key_encrypted IS NOT NULL
ON CONFLICT (ai_integration_config_id, schedule) DO NOTHING;

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
JOIN ai_integration_syncs s
  ON s.ai_integration_config_id = c.id
 AND (s.schedule = c.provider OR s.schedule IS NULL)
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
WHERE ai_integration_config_id = @ai_integration_config_id
  AND schedule = @schedule;

-- name: ListUsagePollCandidates :many
SELECT
    c.id
  , c.organization_id
  , om.slug AS organization_slug
  , c.provider
  , s.schedule
  , s.kind
FROM ai_integration_syncs s
JOIN ai_integration_configs c ON c.id = s.ai_integration_config_id
JOIN organization_metadata om ON om.id = c.organization_id
WHERE c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND c.api_key_encrypted IS NOT NULL
  AND s.next_poll_after <= @poll_due_before
ORDER BY s.next_poll_after ASC, c.organization_id ASC, s.schedule ASC
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
JOIN ai_integration_syncs s
  ON s.ai_integration_config_id = c.id
 AND s.schedule = @schedule
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
WHERE ai_integration_config_id = @ai_integration_config_id
  AND schedule = @schedule;

-- name: AdvanceUsagePollCursor :exec
UPDATE ai_integration_syncs
SET last_cursor_id = @last_cursor_id,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id
  AND schedule = @schedule;

-- name: RecordUsagePollFailure :exec
UPDATE ai_integration_syncs
SET next_poll_after = @next_poll_after,
    last_poll_error = @last_poll_error,
    last_poll_failed_at = clock_timestamp(),
    consecutive_failures = consecutive_failures + 1,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id
  AND schedule = @schedule;

-- name: AdvancePollWatermark :exec
UPDATE ai_integration_syncs
SET poll_watermark_at = @poll_watermark_at,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id
  AND schedule = @schedule;

-- RecordPollSuccessKeepWatermark reschedules a sync and clears failure state
-- without touching the watermark or cursor. Used by schedules that advance
-- poll_watermark_at incrementally mid-sync (e.g. anthropic_analytics) rather
-- than once at the end of a successful poll.
-- name: RecordPollSuccessKeepWatermark :exec
UPDATE ai_integration_syncs
SET next_poll_after = @next_poll_after,
    last_poll_error = NULL,
    last_poll_failed_at = NULL,
    last_poll_success_at = clock_timestamp(),
    consecutive_failures = 0,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id
  AND schedule = @schedule;

-- name: ListSyncSchedules :many
SELECT schedule, kind
FROM ai_integration_syncs
WHERE ai_integration_config_id = @ai_integration_config_id
ORDER BY schedule;

-- InsertPreScheduleSyncRowForTest simulates a sync row written by code that
-- predates the schedule columns: NULL schedule/kind, already due. Test
-- fixture only.
-- name: InsertPreScheduleSyncRowForTest :exec
INSERT INTO ai_integration_syncs (ai_integration_config_id, next_poll_after)
VALUES (@ai_integration_config_id, clock_timestamp() - interval '1 hour');

-- name: CountSyncRowsForTest :one
SELECT count(*)
FROM ai_integration_syncs
WHERE ai_integration_config_id = @ai_integration_config_id;

-- The primary sync schedule shares its name with the config's provider, so
-- config-level reads join on s.schedule = c.provider. Secondary schedules
-- (e.g. anthropic_analytics) are read by their own queries.

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

-- EnsurePrimarySync works while either the legacy config-only unique index or
-- the schedule-aware composite index exists. A targetless conflict clause
-- avoids coupling this writer to either index.
--
-- PostgreSQL statement snapshots do not see a row inserted by a concurrent
-- transaction after this statement starts. The caller retries pgx.ErrNoRows
-- in a fresh statement so a conflict loser can read the committed row.
-- name: EnsurePrimarySync :one
WITH config AS MATERIALIZED (
  SELECT id, provider
  FROM ai_integration_configs
  WHERE ai_integration_configs.id = @ai_integration_config_id
    AND ai_integration_configs.project_id = @project_id
),
updated AS (
  UPDATE ai_integration_syncs s
  SET schedule = COALESCE(s.schedule, c.provider),
      kind = COALESCE(
        s.kind,
        CASE c.provider
          WHEN 'anthropic_compliance' THEN 'cursor'
          ELSE 'time'
        END
      ),
      updated_at = clock_timestamp()
  FROM config c
  WHERE s.ai_integration_config_id = c.id
    AND (s.schedule = c.provider OR s.schedule IS NULL)
    AND (s.schedule IS NULL OR s.kind IS NULL)
  RETURNING s.*
),
inserted AS (
  INSERT INTO ai_integration_syncs (
      ai_integration_config_id
    , schedule
    , kind
  )
  SELECT
      c.id
    , c.provider
    , CASE c.provider
        WHEN 'anthropic_compliance' THEN 'cursor'
        ELSE 'time'
      END
  FROM config c
  WHERE NOT EXISTS (SELECT 1 FROM updated)
    AND NOT EXISTS (
      SELECT 1
      FROM ai_integration_syncs s
      WHERE s.ai_integration_config_id = c.id
        AND (s.schedule = c.provider OR s.schedule IS NULL)
    )
  ON CONFLICT DO NOTHING
  RETURNING *
)
SELECT *
FROM updated
UNION ALL
SELECT *
FROM inserted
UNION ALL
SELECT s.*
FROM ai_integration_syncs s
JOIN config c
  ON s.ai_integration_config_id = c.id
 AND (s.schedule = c.provider OR s.schedule IS NULL)
WHERE NOT EXISTS (SELECT 1 FROM updated)
  AND NOT EXISTS (SELECT 1 FROM inserted)
LIMIT 1;

-- EnsureSync creates an independent secondary schedule after the composite
-- unique index is available. Primary schedules must use EnsurePrimarySync.
-- name: EnsureSync :one
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
ON CONFLICT (ai_integration_config_id, schedule) DO UPDATE
SET kind = COALESCE(ai_integration_syncs.kind, EXCLUDED.kind)
RETURNING *;

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
  AND (schedule = sqlc.arg('schedule')::text OR schedule IS NULL);

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
 AND (
      s.schedule = CASE
        WHEN sqlc.arg('schedule')::text = '' THEN c.provider
        ELSE sqlc.arg('schedule')::text
      END
      OR (
        s.schedule IS NULL
        AND CASE
          WHEN sqlc.arg('schedule')::text = '' THEN c.provider
          ELSE sqlc.arg('schedule')::text
        END = c.provider
      )
    )
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
  AND (
    schedule = sqlc.arg('schedule')::text
    OR (
      schedule IS NULL
      AND EXISTS (
        SELECT 1
        FROM ai_integration_configs c
        WHERE c.id = ai_integration_config_id
          AND c.provider = sqlc.arg('schedule')::text
      )
    )
  );

-- name: AdvanceUsagePollCursor :exec
UPDATE ai_integration_syncs
SET last_cursor_id = @last_cursor_id,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id
  AND (
    schedule = sqlc.arg('schedule')::text
    OR (
      schedule IS NULL
      AND EXISTS (
        SELECT 1
        FROM ai_integration_configs c
        WHERE c.id = ai_integration_config_id
          AND c.provider = sqlc.arg('schedule')::text
      )
    )
  );

-- name: RecordUsagePollFailure :exec
UPDATE ai_integration_syncs
SET next_poll_after = @next_poll_after,
    last_poll_error = @last_poll_error,
    last_poll_failed_at = clock_timestamp(),
    consecutive_failures = consecutive_failures + 1,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id
  AND (
    schedule = sqlc.arg('schedule')::text
    OR (
      schedule IS NULL
      AND EXISTS (
        SELECT 1
        FROM ai_integration_configs c
        WHERE c.id = ai_integration_config_id
          AND c.provider = sqlc.arg('schedule')::text
      )
    )
  );

-- name: AdvancePollWatermark :exec
UPDATE ai_integration_syncs
SET poll_watermark_at = @poll_watermark_at,
    updated_at = clock_timestamp()
WHERE ai_integration_config_id = @ai_integration_config_id
  AND schedule = @schedule;

-- GetSyncScheduleBackfillStatus reports remaining work for one project.
-- Primary rows need discriminators populated; active Anthropic configs also
-- need independent usage and cost schedules.
-- name: GetSyncScheduleBackfillStatus :one
WITH expected_anthropic_schedules AS (
  SELECT
      c.id AS ai_integration_config_id
    , expected.schedule
  FROM ai_integration_configs c
  CROSS JOIN unnest(ARRAY[
      'anthropic_analytics_usage'::text
    , 'anthropic_analytics_cost'::text
  ]) AS expected(schedule)
  WHERE c.project_id = @project_id
    AND c.provider = 'anthropic_compliance'
    AND c.deleted IS FALSE
)
SELECT
    (
      SELECT count(*)
      FROM ai_integration_syncs s
      JOIN ai_integration_configs c ON c.id = s.ai_integration_config_id
      WHERE c.project_id = @project_id
        AND (s.schedule = c.provider OR s.schedule IS NULL)
        AND (s.schedule IS NULL OR s.kind IS NULL)
    )::bigint AS primary_syncs_pending
  , (
      SELECT count(*)
      FROM expected_anthropic_schedules expected
      LEFT JOIN ai_integration_syncs s
        ON s.ai_integration_config_id = expected.ai_integration_config_id
       AND s.schedule = expected.schedule
      WHERE s.id IS NULL
    )::bigint AS analytics_syncs_pending;

-- BackfillSyncSchedulesBatch is the resumable, project-scoped application data
-- migration. Re-running from the zero UUID is safe: discriminator writes are
-- fill-only and secondary inserts use the composite unique index.
-- name: BackfillSyncSchedulesBatch :many
WITH candidate_configs AS MATERIALIZED (
  SELECT
      c.id
    , c.provider
  FROM ai_integration_configs c
  WHERE c.project_id = @project_id
    AND c.id > @after_config_id
    AND (
      EXISTS (
        SELECT 1
        FROM ai_integration_syncs s
        WHERE s.ai_integration_config_id = c.id
          AND (s.schedule = c.provider OR s.schedule IS NULL)
          AND (s.schedule IS NULL OR s.kind IS NULL)
      )
      OR (
        c.provider = 'anthropic_compliance'
        AND c.deleted IS FALSE
        AND EXISTS (
          SELECT 1
          FROM unnest(ARRAY[
              'anthropic_analytics_usage'::text
            , 'anthropic_analytics_cost'::text
          ]) AS expected(schedule)
          WHERE NOT EXISTS (
            SELECT 1
            FROM ai_integration_syncs s
            WHERE s.ai_integration_config_id = c.id
              AND s.schedule = expected.schedule
          )
        )
      )
    )
  ORDER BY c.id
  LIMIT @limit_count
),
updated_primary AS (
  UPDATE ai_integration_syncs s
  SET schedule = COALESCE(s.schedule, c.provider),
      kind = COALESCE(
        s.kind,
        CASE c.provider
          WHEN 'anthropic_compliance' THEN 'cursor'
          ELSE 'time'
        END
      ),
      updated_at = clock_timestamp()
  FROM candidate_configs c
  WHERE s.ai_integration_config_id = c.id
    AND (s.schedule = c.provider OR s.schedule IS NULL)
    AND (s.schedule IS NULL OR s.kind IS NULL)
  RETURNING s.ai_integration_config_id
),
inserted_analytics AS (
  INSERT INTO ai_integration_syncs (
      ai_integration_config_id
    , schedule
    , kind
    , poll_watermark_at
    , next_poll_after
  )
  SELECT
      c.id
    , expected.schedule
    , 'time'
    , TIMESTAMPTZ '1970-01-01 00:00:00+00'
    , TIMESTAMPTZ '1970-01-01 00:00:00+00'
  FROM candidate_configs c
  CROSS JOIN unnest(ARRAY[
      'anthropic_analytics_usage'::text
    , 'anthropic_analytics_cost'::text
  ]) AS expected(schedule)
  WHERE c.provider = 'anthropic_compliance'
  ON CONFLICT (ai_integration_config_id, schedule) DO NOTHING
  RETURNING ai_integration_config_id
)
SELECT
    c.id AS ai_integration_config_id
  , EXISTS (
      SELECT 1
      FROM updated_primary updated
      WHERE updated.ai_integration_config_id = c.id
    ) AS updated_primary
  , (
      SELECT count(*)
      FROM inserted_analytics inserted
      WHERE inserted.ai_integration_config_id = c.id
    )::bigint AS inserted_analytics
FROM candidate_configs c
ORDER BY c.id;

-- Test-only fixtures for transitional sync-row behavior.
-- name: DeleteSecondarySyncSchedulesForTest :exec
DELETE FROM ai_integration_syncs s
USING ai_integration_configs c
WHERE s.ai_integration_config_id = c.id
  AND c.id = @ai_integration_config_id
  AND c.project_id = @project_id
  AND s.schedule <> @primary_schedule;

-- name: DeleteSyncRowsForTest :exec
DELETE FROM ai_integration_syncs s
USING ai_integration_configs c
WHERE s.ai_integration_config_id = c.id
  AND c.id = @ai_integration_config_id
  AND c.project_id = @project_id;

-- name: CountSyncRowsForTest :one
SELECT count(*)::bigint
FROM ai_integration_syncs s
JOIN ai_integration_configs c ON c.id = s.ai_integration_config_id
WHERE c.id = @ai_integration_config_id
  AND c.project_id = @project_id;

-- name: ListSyncSchedules :many
SELECT schedule, kind
FROM ai_integration_syncs
WHERE ai_integration_config_id = @ai_integration_config_id
ORDER BY schedule;

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

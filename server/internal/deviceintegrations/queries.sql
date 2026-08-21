-- Device integration configs are org-scoped (one row per org and provider),
-- so queries here scope by organization_id rather than project_id, matching
-- the aiintegrations service.

-- name: GetConfigByOrgAndProvider :one
SELECT *
FROM device_integration_configs
WHERE organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE;

-- AcquireConfigUpsertLock serializes upserts for one (org, provider) even
-- when no row exists yet — FOR UPDATE cannot lock an absent row, so two
-- concurrent first-time saves would otherwise race to a unique violation.
-- Transaction-scoped: released automatically at commit/rollback.

-- name: AcquireConfigUpsertLock :exec
SELECT pg_advisory_xact_lock(hashtextextended('device_integration_configs:' || @organization_id::text || ':' || @provider::text, 0));

-- GetConfigByOrgAndProviderForUpdate locks the row for the upsert
-- transaction so two concurrent partial saves serialize: the settings merge
-- reads a snapshot that cannot be replaced under it.

-- name: GetConfigByOrgAndProviderForUpdate :one
SELECT *
FROM device_integration_configs
WHERE organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
FOR UPDATE;

-- name: InsertConfig :one
INSERT INTO device_integration_configs (
    organization_id
  , provider
  , credentials_encrypted
  , settings
  , enabled
) VALUES (
    @organization_id
  , @provider
  , @credentials_encrypted
  , @settings
  , @enabled
)
RETURNING *;

-- Credential rotation updates the row in place rather than replacing it:
-- mdm_devices hang off the config id, and rotating a secret must not orphan
-- the synced fleet inventory.

-- name: UpdateConfigCredentials :one
UPDATE device_integration_configs
SET credentials_encrypted = @credentials_encrypted,
    settings = @settings,
    enabled = @enabled,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
RETURNING *;

-- name: UpdateConfigSettings :one
UPDATE device_integration_configs
SET settings = @settings,
    enabled = @enabled,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteConfig :exec
UPDATE device_integration_configs
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND provider = @provider
  AND deleted IS FALSE;

-- name: EnsureSchedule :one
WITH inserted AS (
  INSERT INTO device_integration_schedules (
      device_integration_config_id
    , schedule
  ) VALUES (
      @device_integration_config_id
    , @schedule
  )
  ON CONFLICT (device_integration_config_id, schedule) DO NOTHING
  RETURNING *
)
SELECT *
FROM inserted
UNION ALL
SELECT *
FROM device_integration_schedules
WHERE device_integration_config_id = @device_integration_config_id
  AND schedule = @schedule
LIMIT 1;

-- EnsureSync seeds the sync row due immediately. Both timestamps come from
-- the database clock: due-ness is compared against clock_timestamp() in
-- ListSyncCandidates, and an app-clock value here would make fresh syncs
-- invisible under app/database clock skew.

-- name: EnsureSync :exec
INSERT INTO device_integration_syncs (
    device_integration_schedule_id
  , poll_watermark_at
  , next_poll_after
) VALUES (
    @device_integration_schedule_id
  , clock_timestamp()
  , clock_timestamp()
)
ON CONFLICT (device_integration_schedule_id) DO NOTHING;

-- name: ListSchedulesWithSync :many
SELECT
    sch.id AS schedule_id
  , sch.schedule
  , sch.disabled_at
  , s.poll_watermark_at
  , s.next_poll_after
  , s.last_poll_success_at
  , s.last_poll_failed_at
  , s.last_poll_error
  , s.consecutive_failures
  , s.auto_paused_at
FROM device_integration_schedules sch
JOIN device_integration_syncs s
  ON s.device_integration_schedule_id = sch.id
WHERE sch.device_integration_config_id = @device_integration_config_id
ORDER BY sch.schedule;

-- name: GetScheduleWithSync :one
SELECT
    sch.id AS schedule_id
  , sch.schedule
  , sch.disabled_at
  , s.poll_watermark_at
  , s.next_poll_after
  , s.last_poll_success_at
  , s.last_poll_failed_at
  , s.last_poll_error
  , s.consecutive_failures
  , s.auto_paused_at
FROM device_integration_schedules sch
JOIN device_integration_syncs s
  ON s.device_integration_schedule_id = sch.id
WHERE sch.device_integration_config_id = @device_integration_config_id
  AND sch.schedule = @schedule;

-- SetScheduleDisabled writes user intent onto the schedule row. Only this
-- query (driven by the user) touches disabled_at; config saves and poller
-- writes never do.

-- name: SetScheduleDisabled :one
UPDATE device_integration_schedules
SET disabled_at = CASE WHEN @disabled::bool THEN clock_timestamp() ELSE NULL END,
    updated_at = clock_timestamp()
WHERE device_integration_config_id = @device_integration_config_id
  AND schedule = @schedule
RETURNING *;

-- ClearAutoPauses lifts machine-initiated pauses across a config's sync rows.
-- Saving the integration is the user's "try again" signal; user-initiated
-- disabled_at on the schedule rows is deliberately untouched.

-- name: ClearAutoPauses :exec
UPDATE device_integration_syncs s
SET auto_paused_at = NULL,
    consecutive_failures = 0,
    updated_at = clock_timestamp()
FROM device_integration_schedules sch
WHERE s.device_integration_schedule_id = sch.id
  AND sch.device_integration_config_id = @device_integration_config_id
  AND s.auto_paused_at IS NOT NULL;

-- ResetSyncStateForConfig gives a config's schedules a clean slate after a
-- credential rotation: stale failure state must not keep rendering "failed"
-- after the admin fixed the credential, and last_push_digest must not
-- short-circuit the first push to a newly pointed-at vendor account. The
-- watermark is deliberately untouched (it is the mark-missing cutoff, and
-- device inventory continuity survives rotation); user disabled_at lives on
-- the schedule rows and is never touched here.

-- name: ResetSyncStateForConfig :exec
UPDATE device_integration_syncs s
SET auto_paused_at = NULL,
    consecutive_failures = 0,
    consecutive_auth_rejections = 0,
    last_poll_error = NULL,
    last_poll_failed_at = NULL,
    last_push_digest = NULL,
    next_poll_after = clock_timestamp(),
    updated_at = clock_timestamp()
FROM device_integration_schedules sch
WHERE s.device_integration_schedule_id = sch.id
  AND sch.device_integration_config_id = @device_integration_config_id;

-- Enabling a connection means "sync now": mark every schedule due without
-- disturbing failure history or the push digest (unlike the full reset a
-- credential rotation performs).
-- name: MarkConfigSyncsDue :exec
UPDATE device_integration_syncs s
SET next_poll_after = clock_timestamp(),
    updated_at = clock_timestamp()
FROM device_integration_schedules sch
WHERE s.device_integration_schedule_id = sch.id
  AND sch.device_integration_config_id = @device_integration_config_id;

-- RetrySchedule makes one schedule due immediately, lifting any automatic
-- pause and clearing its failure state. The stored error is cleared
-- deliberately — the user acknowledged it by retrying, and a failing sync
-- re-records it. A user-disabled schedule stays disabled.

-- name: RetrySchedule :one
UPDATE device_integration_syncs s
SET auto_paused_at = NULL,
    consecutive_failures = 0,
    consecutive_auth_rejections = 0,
    last_poll_error = NULL,
    last_poll_failed_at = NULL,
    next_poll_after = clock_timestamp(),
    updated_at = clock_timestamp()
FROM device_integration_schedules sch
WHERE s.device_integration_schedule_id = sch.id
  AND sch.device_integration_config_id = @device_integration_config_id
  AND sch.schedule = @schedule
RETURNING s.*;

-- Coverage classifies each present (non-missing) device of the org's live
-- configs against agent heartbeats. There are two matching modes, selected by
-- the @device_level parameter (the caller resolves feature.FlagDeviceLevelCoverage):
--
--   device-level  the device's hardware serial against device_agent_device_syncs
--                 (LOWER(serial_number) on both sides), falling back to email
--   user-level    the MDM-reported assigned-user email against device_agent_syncs
--                 (LOWER(email) on both sides) — the pre-DNO-643 behavior
--
-- Buckets, in precedence order:
--
--   missing             the device stopped appearing in MDM inventory
--   agent_active        the agent heartbeat is within the window
--   agent_stale         an agent is known, but it went quiet (drift)
--   agent_other_device  device-level only: the assigned user runs the agent,
--                       just not on this machine
--   no_email            the MDM reported no assigned-user email
--   no_agent            email resolves to an org member with no agent at all
--   unresolved_email    email matches neither an agent user nor an org member
--
-- Two precedence details matter. A serial match outranks no_email: a device
-- with no assigned user in the MDM is still legible when its own agent reports
-- in, which is most of what device-level matching buys. And agent_other_device
-- is deliberately distinct from no_agent — "installed, wrong machine" and
-- "never installed" need different remediation.
--
-- Under user-level matching the heartbeat attests only that the assigned USER
-- runs the agent somewhere, never that this device does.

-- name: GetCoverageCounts :one
-- Counts read the lateral's bucket rather than restating its predicates, so
-- the aggregate and the per-row list in ListManagedDevices cannot disagree.
SELECT
    count(*) FILTER (WHERE cov.coverage_bucket = 'missing') AS missing
  , count(*) FILTER (WHERE cov.coverage_bucket = 'no_email') AS no_email
  , count(*) FILTER (WHERE cov.coverage_bucket = 'agent_active') AS agent_active
  , count(*) FILTER (WHERE cov.coverage_bucket = 'agent_stale') AS agent_stale
  , count(*) FILTER (WHERE cov.coverage_bucket = 'agent_other_device') AS agent_other_device
    -- How many of the agent_active devices are backed by their OWN heartbeat.
    -- agent_active is reachable via the email fallback even in device-level
    -- mode, so without this the caller cannot tell whether "active" means
    -- "this machine reported" for every device or only for some — and would
    -- have to state the strong claim for all of them.
    -- The bucket guard is load-bearing, not decoration. A device retired from
    -- MDM keeps bucket 'missing' while its agent may still be installed and
    -- reporting, so without it this count could EXCEED agent_active and the
    -- caller's equality check would never match again — permanently printing
    -- the weaker claim for an org whose every active device is serial-attested.
  , count(*) FILTER (WHERE cov.coverage_bucket = 'agent_active' AND @device_level::boolean AND dads.last_seen_at >= @active_cutoff::timestamptz) AS agent_active_device_attested
  , count(*) FILTER (WHERE cov.coverage_bucket = 'no_agent') AS no_agent
  , count(*) FILTER (WHERE cov.coverage_bucket = 'unresolved_email') AS unresolved_email
  , count(*) AS total
FROM mdm_devices d
JOIN device_integration_configs c
  ON c.id = d.device_integration_config_id
 AND c.deleted IS FALSE
LEFT JOIN device_agent_syncs das
  ON das.organization_id = d.organization_id
 AND LOWER(das.email) = LOWER(d.user_email)
LEFT JOIN device_agent_device_syncs dads
  ON dads.organization_id = d.organization_id
 AND LOWER(dads.serial_number) = LOWER(d.serial_number)
-- Positive evidence for the agent_other_device bucket: a fresh heartbeat from
-- this user on a DIFFERENT machine. LIMIT 1 because existence is the whole
-- question.
LEFT JOIN LATERAL (
  SELECT 1 AS found
  FROM device_agent_device_syncs o
  WHERE o.organization_id = d.organization_id
    AND LOWER(o.email) = LOWER(d.user_email)
    AND LOWER(o.serial_number) IS DISTINCT FROM LOWER(d.serial_number)
    AND o.last_seen_at >= @active_cutoff::timestamptz
  LIMIT 1
) other ON TRUE
CROSS JOIN LATERAL (
  SELECT CASE
    WHEN d.missing_since IS NOT NULL THEN 'missing'
    -- Device-level: this machine's own heartbeat answers for it, and outranks
    -- everything email-based including no_email — a device the MDM records
    -- with no assigned user is still legible when its own agent reports in.
    WHEN @device_level::boolean AND dads.last_seen_at >= @active_cutoff::timestamptz THEN 'agent_active'
    WHEN @device_level::boolean AND dads.id IS NOT NULL THEN 'agent_stale'
    -- Device-level only, and it requires POSITIVE evidence: the assigned user
    -- has a fresh heartbeat from a different serial-identified machine, so
    -- this one is a real gap. Without that evidence we must NOT land here —
    -- an agent that reports no serial is indistinguishable from one running
    -- on another machine, and guessing "elsewhere" would tell an admin to
    -- reinstall on a device already running the agent.
    WHEN @device_level::boolean AND dads.id IS NULL AND other.found IS NOT NULL THEN 'agent_other_device'
    -- Shared fallback, identical in both modes. This is what keeps the two
    -- consistent: a device with only a user-level heartbeat stays
    -- agent_active (the evidence push says the same thing, at "user"
    -- attestation), so enabling device-level matching can never demote a
    -- covered fleet to 0%.
    WHEN coalesce(d.user_email, '') = '' THEN 'no_email'
    WHEN das.last_seen_at >= @active_cutoff::timestamptz THEN 'agent_active'
    WHEN das.id IS NOT NULL THEN 'agent_stale'
    WHEN d.user_id IS NOT NULL THEN 'no_agent'
    ELSE 'unresolved_email'
  END AS coverage_bucket
) cov
WHERE d.organization_id = @organization_id
  AND (sqlc.narg('provider')::text IS NULL OR c.provider = sqlc.narg('provider')::text);

-- When scoped to one provider, "unmanaged" means no managed device from THAT
-- provider — an agent user covered only by a different MDM still counts.

-- name: CountUnmanagedAgentUsers :one
SELECT count(*)
FROM device_agent_syncs das
WHERE das.organization_id = @organization_id
  AND NOT EXISTS (
    SELECT 1
    FROM mdm_devices d
    JOIN device_integration_configs c
      ON c.id = d.device_integration_config_id
     AND c.deleted IS FALSE
    WHERE d.organization_id = das.organization_id
      AND d.missing_since IS NULL
      AND LOWER(d.user_email) = LOWER(das.email)
      AND (sqlc.narg('provider')::text IS NULL OR c.provider = sqlc.narg('provider')::text)
  )
  -- Under device-level matching the email link is no longer the only way an
  -- agent user can be managed: one of their machines may be in inventory
  -- under a different email, or none at all. Without this escape hatch the
  -- tile would report a user as having no managed device while the coverage
  -- list shows that very device as active.
  -- Written as a top-level NOT EXISTS with the flag INSIDE, not as
  -- NOT (@device_level AND EXISTS (...)). Nesting the sublink under NOT(AND ...)
  -- blocks pull_up_sublinks (it runs before const-folding, so even a literal
  -- true does not help): Postgres falls back to a hashed SubPlan and hoists the
  -- correlation quals into hash keys, leaving the scans with no organization_id
  -- predicate — so it reads EVERY tenant's rows on a user-facing dashboard
  -- call. At top level it plans as an org-scoped anti-join over the serial
  -- indexes, and degenerates to a cheap constant-false scan when the flag is off.
  AND NOT EXISTS (
    SELECT 1
    FROM device_agent_device_syncs dads
    JOIN mdm_devices d2
      ON d2.organization_id = dads.organization_id
     AND LOWER(d2.serial_number) = LOWER(dads.serial_number)
     AND d2.missing_since IS NULL
    JOIN device_integration_configs c2
      ON c2.id = d2.device_integration_config_id
     AND c2.deleted IS FALSE
    WHERE @device_level::boolean
      AND dads.organization_id = @organization_id
      AND LOWER(dads.email) = LOWER(das.email)
      AND (sqlc.narg('provider')::text IS NULL OR c2.provider = sqlc.narg('provider')::text)
  );

-- ListManagedDevices pages the org's device inventory newest-first by id
-- (UUIDv7, so id order is creation order) with an `id <` cursor, computing
-- each device's coverage bucket with the same rules as GetCoverageCounts.
-- The optional bucket filter accepts the bucket names plus 'missing'.

-- name: ListManagedDevices :many
SELECT
    d.*
  , c.provider
    -- Show the heartbeat that classified the row: under device-level matching
    -- a serial match is what makes the device active, so surfacing the user's
    -- timestamp instead would contradict the bucket beside it.
  , (CASE
      WHEN @device_level::boolean AND dads.id IS NOT NULL THEN dads.last_seen_at
      ELSE das.last_seen_at
    END)::timestamptz AS agent_last_seen_at
  , cov.coverage_bucket
FROM mdm_devices d
JOIN device_integration_configs c
  ON c.id = d.device_integration_config_id
 AND c.deleted IS FALSE
LEFT JOIN device_agent_syncs das
  ON das.organization_id = d.organization_id
 AND LOWER(das.email) = LOWER(d.user_email)
LEFT JOIN device_agent_device_syncs dads
  ON dads.organization_id = d.organization_id
 AND LOWER(dads.serial_number) = LOWER(d.serial_number)
-- Positive evidence for the agent_other_device bucket: a fresh heartbeat from
-- this user on a DIFFERENT machine. LIMIT 1 because existence is the whole
-- question.
LEFT JOIN LATERAL (
  SELECT 1 AS found
  FROM device_agent_device_syncs o
  WHERE o.organization_id = d.organization_id
    AND LOWER(o.email) = LOWER(d.user_email)
    AND LOWER(o.serial_number) IS DISTINCT FROM LOWER(d.serial_number)
    AND o.last_seen_at >= @active_cutoff::timestamptz
  LIMIT 1
) other ON TRUE
CROSS JOIN LATERAL (
  -- Kept byte-identical to the CASE in GetCoverageCounts. Both the projected
  -- column and the bucket filter below read this alias, so what is shown and
  -- what is filtered cannot drift apart.
  SELECT CASE
    WHEN d.missing_since IS NOT NULL THEN 'missing'
    -- Device-level: this machine's own heartbeat answers for it, and outranks
    -- everything email-based including no_email — a device the MDM records
    -- with no assigned user is still legible when its own agent reports in.
    WHEN @device_level::boolean AND dads.last_seen_at >= @active_cutoff::timestamptz THEN 'agent_active'
    WHEN @device_level::boolean AND dads.id IS NOT NULL THEN 'agent_stale'
    -- Device-level only, and it requires POSITIVE evidence: the assigned user
    -- has a fresh heartbeat from a different serial-identified machine, so
    -- this one is a real gap. Without that evidence we must NOT land here —
    -- an agent that reports no serial is indistinguishable from one running
    -- on another machine, and guessing "elsewhere" would tell an admin to
    -- reinstall on a device already running the agent.
    WHEN @device_level::boolean AND dads.id IS NULL AND other.found IS NOT NULL THEN 'agent_other_device'
    -- Shared fallback, identical in both modes. This is what keeps the two
    -- consistent: a device with only a user-level heartbeat stays
    -- agent_active (the evidence push says the same thing, at "user"
    -- attestation), so enabling device-level matching can never demote a
    -- covered fleet to 0%.
    WHEN coalesce(d.user_email, '') = '' THEN 'no_email'
    WHEN das.last_seen_at >= @active_cutoff::timestamptz THEN 'agent_active'
    WHEN das.id IS NOT NULL THEN 'agent_stale'
    WHEN d.user_id IS NOT NULL THEN 'no_agent'
    ELSE 'unresolved_email'
  END AS coverage_bucket
) cov
WHERE d.organization_id = @organization_id
  AND (sqlc.narg('provider')::text IS NULL OR c.provider = sqlc.narg('provider')::text)
  AND (sqlc.narg('cursor_id')::uuid IS NULL OR d.id < sqlc.narg('cursor_id')::uuid)
  AND (sqlc.narg('bucket')::text IS NULL OR sqlc.narg('bucket')::text = cov.coverage_bucket)
  -- One person's devices. Both legs are needed and neither subsumes the other:
  -- user_id is only set when the MDM's reported email resolved to a member, so
  -- matching on it alone would drop the devices of someone whose MDM email is
  -- a work alias, while matching on email alone would drop a device whose
  -- assigned email the MDM has since changed.
  AND (
    (COALESCE(cardinality(@user_ids::text[]), 0) = 0 AND COALESCE(cardinality(@user_emails::text[]), 0) = 0)
    OR d.user_id = ANY(@user_ids::text[])
    OR LOWER(d.user_email) = ANY(ARRAY(SELECT LOWER(e) FROM unnest(@user_emails::text[]) AS e))
  )
ORDER BY d.id DESC
LIMIT @page_limit;

-- Scheduling: the Temporal coordinator selects due syncs, and the sync runner
-- records outcomes. Workflow payloads carry only sync ids — the runner loads
-- config and decrypts credentials inside the activity, never in Temporal
-- history.

-- DBNow anchors sync-start cutoffs to the database clock: mdm_devices rows
-- are stamped with clock_timestamp(), so comparing them against an
-- app-server timestamp would mis-mark devices whenever the two clocks skew.

-- name: DBNow :one
SELECT clock_timestamp()::timestamptz AS now;

-- name: ListSyncCandidates :many
SELECT
    s.id AS sync_id
  , c.organization_id
  , om.slug AS organization_slug
  , c.provider
  , sch.schedule
FROM device_integration_syncs s
JOIN device_integration_schedules sch
  ON sch.id = s.device_integration_schedule_id
JOIN device_integration_configs c
  ON c.id = sch.device_integration_config_id
JOIN organization_metadata om
  ON om.id = c.organization_id
WHERE c.enabled IS TRUE
  AND c.deleted IS FALSE
  AND sch.disabled_at IS NULL
  AND s.auto_paused_at IS NULL
  AND s.next_poll_after <= clock_timestamp()
  AND NOT (s.id = ANY (@exclude_sync_ids::uuid[]))
ORDER BY s.next_poll_after ASC, c.organization_id ASC, sch.schedule ASC
LIMIT @limit_count;

-- name: GetSyncTarget :one
SELECT
    c.id AS config_id
  , c.organization_id
  , c.provider
  , c.credentials_encrypted
  , c.settings
  , c.enabled
  , c.deleted
  , c.updated_at AS config_updated_at
  , sch.schedule
  , sch.disabled_at
  , s.id AS sync_id
  , s.poll_watermark_at
  , s.consecutive_failures
  , s.auto_paused_at
  , s.last_push_digest
FROM device_integration_syncs s
JOIN device_integration_schedules sch
  ON sch.id = s.device_integration_schedule_id
JOIN device_integration_configs c
  ON c.id = sch.device_integration_config_id
WHERE s.id = @sync_id;

-- RecordSyncSuccess reschedules a sync and clears its failure state — the
-- error fields clear on success by contract, so scheduleStatus's recency
-- derivation renders recovery as success. last_push_digest only moves when
-- the caller supplies one (evidence pushes); inventory syncs leave it alone.
-- The write is guarded on the config's updated_at as observed when the sync
-- started: a config save (rotation, settings change) invalidates in-flight
-- outcomes so a pre-save sync cannot clobber the post-save clean slate.
-- next_poll_after is computed on the database clock so all scheduler time
-- arithmetic lives in one clock domain.

-- name: RecordSyncSuccess :execrows
UPDATE device_integration_syncs s
SET next_poll_after = clock_timestamp() + make_interval(secs => @next_in_seconds::int),
    poll_watermark_at = COALESCE(sqlc.narg('poll_watermark_at')::timestamptz, s.poll_watermark_at),
    last_poll_success_at = clock_timestamp(),
    last_poll_error = NULL,
    last_poll_failed_at = NULL,
    consecutive_failures = 0,
    consecutive_auth_rejections = 0,
    auto_paused_at = NULL,
    last_push_digest = COALESCE(sqlc.narg('last_push_digest')::text, s.last_push_digest),
    updated_at = clock_timestamp()
FROM device_integration_schedules sch
JOIN device_integration_configs c
  ON c.id = sch.device_integration_config_id
WHERE s.id = @sync_id
  AND sch.id = s.device_integration_schedule_id
  AND c.updated_at = @config_updated_at;

-- RecordSyncFailure reschedules with backoff and, when pause_after is
-- positive and the new streak reaches it, auto-pauses the schedule so
-- candidate selection stops re-enqueueing it. Callers pass zero pause_after
-- for failures that should never pause (e.g. transient network errors).
-- Returns whether this call left the schedule auto-paused: the caller loads a
-- runnable (never-paused) sync, so a non-null auto_paused_at can only mean
-- THIS failure crossed the threshold — a clean auto-pause metric signal. Zero
-- rows (a concurrent config save moved updated_at) returns no row.

-- name: RecordSyncFailure :one
UPDATE device_integration_syncs s
SET next_poll_after = clock_timestamp() + make_interval(secs => @next_in_seconds::int),
    last_poll_error = @last_poll_error,
    last_poll_failed_at = clock_timestamp(),
    consecutive_failures = s.consecutive_failures + 1,
    -- The auth-rejection streak is tracked separately: any non-auth failure
    -- resets it, so only a PURE run of credential rejections can reach the
    -- auto-pause threshold.
    consecutive_auth_rejections = CASE
      WHEN @auth_rejection::bool THEN s.consecutive_auth_rejections + 1
      ELSE 0
    END,
    auto_paused_at = CASE
      WHEN @auth_rejection::bool AND @pause_after::int > 0 AND s.consecutive_auth_rejections + 1 >= @pause_after::int
      THEN clock_timestamp()
      ELSE s.auto_paused_at
    END,
    updated_at = clock_timestamp()
FROM device_integration_schedules sch
JOIN device_integration_configs c
  ON c.id = sch.device_integration_config_id
WHERE s.id = @sync_id
  AND sch.id = s.device_integration_schedule_id
  AND c.updated_at = @config_updated_at
RETURNING (s.auto_paused_at IS NOT NULL)::boolean AS auto_paused;

-- UpsertMdmDevice reconciles one inventory row. A reappearing device clears
-- missing_since; last_seen_at stamps this observation for the mark-missing
-- cutoff. The write is guarded on the config's updated_at as observed when
-- the sync started: a config save (rotation, endpoint change) mid-pull makes
-- the insert a zero-row no-op, so stale-credential inventory never merges
-- into the newly saved config.

-- name: UpsertMdmDevice :execrows
INSERT INTO mdm_devices (
    device_integration_config_id
  , organization_id
  , external_id
  , serial_number
  , hostname
  , os_name
  , os_version
  , user_email
  , user_id
  , mdm_last_check_in_at
  , raw
)
SELECT
    @device_integration_config_id
  , @organization_id
  , @external_id
  , sqlc.narg('serial_number')::text
  , sqlc.narg('hostname')::text
  , sqlc.narg('os_name')::text
  , sqlc.narg('os_version')::text
  , sqlc.narg('user_email')::text
  , sqlc.narg('user_id')::text
  , sqlc.narg('mdm_last_check_in_at')::timestamptz
  , @raw
WHERE EXISTS (
  SELECT 1
  FROM device_integration_configs c
  WHERE c.id = @device_integration_config_id
    AND c.updated_at = @config_updated_at
)
ON CONFLICT (device_integration_config_id, external_id) DO UPDATE SET
    serial_number = EXCLUDED.serial_number,
    hostname = EXCLUDED.hostname,
    os_name = EXCLUDED.os_name,
    os_version = EXCLUDED.os_version,
    user_email = EXCLUDED.user_email,
    user_id = EXCLUDED.user_id,
    mdm_last_check_in_at = EXCLUDED.mdm_last_check_in_at,
    raw = EXCLUDED.raw,
    last_seen_at = clock_timestamp(),
    missing_since = NULL,
    updated_at = clock_timestamp();

-- MarkDevicesMissing stamps devices absent from the snapshot that started at
-- @sync_started_at. INVARIANT: only ever called in the same transaction that
-- records the fully completed snapshot (RecordSyncSuccess) — a partial pull
-- must never mark unvisited devices missing.

-- name: MarkDevicesMissing :exec
UPDATE mdm_devices
SET missing_since = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE device_integration_config_id = @device_integration_config_id
  AND missing_since IS NULL
  AND last_seen_at < @sync_started_at;

-- ResolveOrgMemberByEmail maps an MDM-reported email to the org member it
-- belongs to, scoped through the org membership so a same-email user in a
-- different org never links.

-- name: ResolveOrgMemberByEmail :one
SELECT u.id
FROM users u
JOIN organization_user_relationships our
  ON our.user_id = u.id
WHERE our.organization_id = @organization_id
  AND our.deleted IS FALSE
  AND LOWER(u.email) = LOWER(@email)
  AND u.deleted_at IS NULL
ORDER BY u.id
LIMIT 1;

-- ListCoverageSnapshotDevices feeds evidence-sink pushes: every present
-- device across the org's live configs, with the assigned user's agent
-- heartbeat. Ordered by external id so the snapshot digest is deterministic.

-- name: ListCoverageSnapshotDevices :many
-- Prefers the machine's own heartbeat, falling back to its assigned user's.
-- device_attested travels with each row because the answer is per device, not
-- per org: even with device-level matching on, a machine whose agent cannot
-- report a serial is still only user-attested, and pushed evidence must say
-- so rather than let the stronger claim leak across.
SELECT
    d.external_id
  , d.serial_number
  , d.hostname
  , d.user_email
  , (CASE
      WHEN @device_level::boolean AND dads.id IS NOT NULL THEN dads.last_seen_at
      ELSE das.last_seen_at
    END)::timestamptz AS agent_last_seen_at
  , (@device_level::boolean AND dads.id IS NOT NULL) AS device_attested
FROM mdm_devices d
JOIN device_integration_configs c
  ON c.id = d.device_integration_config_id
 AND c.deleted IS FALSE
LEFT JOIN device_agent_syncs das
  ON das.organization_id = d.organization_id
 AND LOWER(das.email) = LOWER(d.user_email)
LEFT JOIN device_agent_device_syncs dads
  ON dads.organization_id = d.organization_id
 AND LOWER(dads.serial_number) = LOWER(d.serial_number)
WHERE d.organization_id = @organization_id
  AND d.missing_since IS NULL
ORDER BY d.external_id ASC, d.id ASC;

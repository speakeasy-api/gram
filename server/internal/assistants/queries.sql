-- name: ReapStuckAssistantRuntimes :many
-- Short-horizon reaper for rows the happy-path can no longer move. Applies
-- to both v1 (per-thread VM) and v2 (single VM per assistant) rows: the
-- starting/active/expiring liveness markers it keys on are version-agnostic.
-- A v2 VM in active use updates warm_until and last_heartbeat_at via every
-- thread workflow, so an in-flight VM is never matched by the active branch;
-- only assistants whose entire thread set has gone idle past the cutoffs are
-- collected.
UPDATE assistant_runtimes
SET
  state = @stopped_state,
  updated_at = clock_timestamp(),
  deleted_at = clock_timestamp()
WHERE deleted IS FALSE
  AND (
    (state = @starting_state AND updated_at < @starting_cutoff)
    OR (
      state = @active_state
      AND warm_until IS NOT NULL
      AND warm_until < @warm_cutoff
      AND COALESCE(last_heartbeat_at, updated_at) < @heartbeat_cutoff
    )
    -- Backstop for activities that exhaust Temporal's retry budget after CAS
    -- active->expiring without reaching Stop. Without this the partial unique
    -- indexes (v1 on assistant_thread_id, v2 on assistant_id) block new
    -- admits indefinitely.
    OR (state = @expiring_state AND updated_at < @expiring_cutoff)
  )
RETURNING assistant_id;

-- name: RequeueStaleAssistantEvents :many
UPDATE assistant_thread_events
SET
  status = @pending_status,
  updated_at = clock_timestamp()
WHERE deleted IS FALSE
  AND status = @processing_status
  AND updated_at < @updated_before
RETURNING assistant_id;

-- name: ResolveThreadProjectID :one
SELECT project_id
FROM assistant_threads
WHERE id = @thread_id;

-- name: LoadAssistantThreadForBootstrap :one
SELECT
  t.id,
  t.assistant_id,
  t.project_id,
  t.correlation_id,
  t.chat_id,
  t.source_kind,
  t.source_ref_json,
  a.organization_id,
  a.created_by_user_id,
  a.name,
  a.model,
  a.instructions,
  a.warm_ttl_seconds,
  a.max_concurrency,
  a.status,
  a.created_at,
  a.updated_at,
  a.deleted_at
FROM assistant_threads t
JOIN assistants a ON a.id = t.assistant_id AND a.project_id = t.project_id
WHERE t.id = @thread_id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
  AND a.deleted IS FALSE;

-- name: ResolveThreadCorrelation :one
SELECT id, project_id, assistant_id, correlation_id
FROM assistant_threads
WHERE id = @thread_id
  AND deleted IS FALSE;

-- name: ResolveToolsetsForWrite :many
SELECT id, slug
FROM toolsets
WHERE project_id = @project_id
  AND slug = ANY(@slugs::TEXT[])
  AND deleted IS FALSE;

-- name: ResolveEnvironmentsForWrite :many
SELECT id, slug
FROM environments
WHERE project_id = @project_id
  AND slug = ANY(@slugs::TEXT[])
  AND deleted IS FALSE;

-- name: LoadAssistantToolsets :many
SELECT
  at.assistant_id,
  at.toolset_id,
  t.slug AS toolset_slug,
  t.mcp_enabled,
  t.mcp_slug,
  t.default_environment_slug,
  at.environment_id,
  e.slug AS environment_slug
FROM assistant_toolsets at
JOIN toolsets t ON t.id = at.toolset_id
LEFT JOIN environments e ON e.id = at.environment_id
WHERE at.assistant_id = ANY(@assistant_ids::UUID[])
  AND at.project_id = @project_id
ORDER BY at.created_at;

-- name: ClearAssistantToolsets :exec
DELETE FROM assistant_toolsets
WHERE assistant_id = @assistant_id
  AND project_id = @project_id;

-- name: AddAssistantToolsets :copyfrom
INSERT INTO assistant_toolsets (
  assistant_id,
  toolset_id,
  environment_id,
  project_id
) VALUES (
  @assistant_id,
  @toolset_id,
  @environment_id,
  @project_id
);

-- name: EnableMCPForToolsets :exec
-- Flips mcp_enabled to TRUE for the listed toolsets in a project. Every
-- toolset attached to an assistant must be MCP-reachable for the runtime's
-- startup config to build; we enable on attach so users don't have to do it
-- separately. mcp_slug is required for an MCP-reachable toolset, so we skip
-- rows that lack one.
UPDATE toolsets
SET mcp_enabled = TRUE,
    updated_at = clock_timestamp()
WHERE id = ANY(@toolset_ids::UUID[])
  AND project_id = @project_id
  AND mcp_enabled IS FALSE
  AND mcp_slug IS NOT NULL
  AND deleted IS FALSE;

-- name: CreateAssistant :one
INSERT INTO assistants (
  project_id,
  organization_id,
  created_by_user_id,
  name,
  model,
  instructions,
  warm_ttl_seconds,
  max_concurrency,
  status
) VALUES (
  @project_id,
  @organization_id,
  @created_by_user_id,
  @name,
  @model,
  @instructions,
  @warm_ttl_seconds,
  @max_concurrency,
  @status
)
RETURNING id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at;

-- name: ListAssistants :many
SELECT id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
FROM assistants
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: GetAssistant :one
SELECT id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
FROM assistants
WHERE id = @assistant_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetAssistantForDispatch :one
SELECT id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
FROM assistants
WHERE id = @assistant_id
  AND deleted IS FALSE;

-- name: UpdateAssistant :one
UPDATE assistants
SET
  name = COALESCE(sqlc.narg('name')::TEXT, name),
  model = COALESCE(sqlc.narg('model')::TEXT, model),
  instructions = COALESCE(sqlc.narg('instructions')::TEXT, instructions),
  warm_ttl_seconds = COALESCE(sqlc.narg('warm_ttl_seconds')::BIGINT, warm_ttl_seconds),
  max_concurrency = COALESCE(sqlc.narg('max_concurrency')::BIGINT, max_concurrency),
  status = COALESCE(sqlc.narg('status')::TEXT, status),
  updated_at = clock_timestamp()
WHERE id = @assistant_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at;

-- name: DeleteAssistant :exec
UPDATE assistants
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @assistant_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: UpsertAssistantChat :exec
INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id, title, created_at, updated_at)
VALUES (@chat_id, @project_id, @organization_id, NULL, NULL, @title, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.id;

-- name: UpsertAssistantThread :one
INSERT INTO assistant_threads (
  assistant_id,
  project_id,
  correlation_id,
  chat_id,
  source_kind,
  source_ref_json,
  last_event_at
) VALUES (
  @assistant_id,
  @project_id,
  @correlation_id,
  @chat_id,
  @source_kind,
  @source_ref_json,
  clock_timestamp()
)
ON CONFLICT (project_id, assistant_id, correlation_id) WHERE deleted IS FALSE
DO UPDATE SET
  source_ref_json = EXCLUDED.source_ref_json,
  last_event_at = clock_timestamp(),
  updated_at = clock_timestamp()
RETURNING id;

-- name: InsertAssistantThreadEvent :one
INSERT INTO assistant_thread_events (
  assistant_thread_id,
  assistant_id,
  project_id,
  trigger_instance_id,
  event_id,
  correlation_id,
  status,
  normalized_payload_json,
  source_payload_json
) VALUES (
  @assistant_thread_id,
  @assistant_id,
  @project_id,
  @trigger_instance_id,
  @event_id,
  @correlation_id,
  @status,
  @normalized_payload_json,
  @source_payload_json
)
ON CONFLICT (project_id, assistant_id, event_id) WHERE deleted IS FALSE DO NOTHING
RETURNING id;

-- name: ListWarmPendingThreads :many
SELECT DISTINCT t.id
FROM assistant_threads t
JOIN assistant_runtimes r
  ON r.assistant_thread_id = t.id
  AND r.project_id = t.project_id
WHERE t.project_id = @project_id
  AND t.assistant_id = @assistant_id
  AND t.deleted IS FALSE
  AND r.deleted IS FALSE
  AND r.ended IS FALSE
  AND r.state = @active_state
  AND (r.warm_until IS NULL OR r.warm_until > clock_timestamp())
  AND EXISTS (
    SELECT 1
    FROM assistant_thread_events e
    WHERE e.project_id = t.project_id
      AND e.assistant_thread_id = t.id
      AND e.deleted IS FALSE
      AND e.status = @pending_status
  );

-- name: CountActiveAssistantRuntimes :one
SELECT COUNT(*)
FROM assistant_runtimes
WHERE project_id = @project_id
  AND assistant_id = @assistant_id
  AND deleted IS FALSE
  AND ended IS FALSE
  AND (
    state = @starting_state
    OR (state = @active_state AND (warm_until IS NULL OR warm_until > clock_timestamp()))
  );

-- name: ListColdPendingThreadsForAdmit :many
SELECT t.id, t.project_id
FROM assistant_threads t
WHERE t.project_id = @project_id
  AND t.assistant_id = @assistant_id
  AND t.deleted IS FALSE
  AND EXISTS (
    SELECT 1
    FROM assistant_thread_events e
    WHERE e.project_id = t.project_id
      AND e.assistant_thread_id = t.id
      AND e.deleted IS FALSE
      AND e.status = @pending_status
  )
  AND NOT EXISTS (
    SELECT 1
    FROM assistant_runtimes r
    WHERE r.project_id = t.project_id
      AND r.assistant_thread_id = t.id
      AND r.deleted IS FALSE
      AND r.ended IS FALSE
      AND (
        r.state = @starting_state
        OR (r.state = @active_state AND (r.warm_until IS NULL OR r.warm_until > clock_timestamp()))
      )
  )
  AND NOT EXISTS (
    SELECT 1
    FROM assistant_runtimes lr
    WHERE lr.project_id = t.project_id
      AND lr.assistant_thread_id = t.id
      AND lr.state = @failed_state
      AND lr.updated_at > @admit_failure_backoff_cutoff
      AND lr.created_at = (
        SELECT MAX(rlatest.created_at)
        FROM assistant_runtimes rlatest
        WHERE rlatest.project_id = t.project_id
          AND rlatest.assistant_thread_id = t.id
      )
  )
ORDER BY (
  SELECT MIN(e.created_at)
  FROM assistant_thread_events e
  WHERE e.project_id = t.project_id
    AND e.assistant_thread_id = t.id
    AND e.deleted IS FALSE
    AND e.status = @pending_status
) ASC
LIMIT @limit_count
FOR UPDATE OF t SKIP LOCKED;

-- name: ReserveAssistantRuntime :exec
INSERT INTO assistant_runtimes (
  assistant_thread_id,
  assistant_id,
  project_id,
  backend,
  state,
  backend_metadata_json
) VALUES (
  @assistant_thread_id,
  @assistant_id,
  @project_id,
  @backend,
  @state,
  COALESCE((
    SELECT r.backend_metadata_json
    FROM assistant_runtimes r
    WHERE r.project_id = @project_id
      AND r.assistant_thread_id = @assistant_thread_id
      AND r.backend = @backend
      AND r.backend_metadata_json <> '{}'::jsonb
    ORDER BY r.created_at DESC
    LIMIT 1
  ), '{}'::jsonb)
)
ON CONFLICT DO NOTHING;

-- name: ReserveAssistantRuntimeV2 :exec
-- v2 runtimes are keyed on (project_id, assistant_id) — one VM serves
-- every thread under an assistant. assistant_thread_id is set to the
-- admitting thread (the one that triggered admit) so the column stays
-- a real reference; the runtime_version = 2 marker carries the v2
-- semantic distinction. The unique partial index
-- `assistant_runtimes_v2_one_per_assistant` backs the ON CONFLICT and
-- guarantees the single-VM invariant under concurrent admit. Callers
-- must hold pg_advisory_xact_lock on the assistant id to serialise VM
-- creation across workers.
INSERT INTO assistant_runtimes (
  assistant_thread_id,
  assistant_id,
  project_id,
  backend,
  state,
  runtime_version,
  backend_metadata_json
) VALUES (
  @assistant_thread_id,
  @assistant_id,
  @project_id,
  @backend,
  @state,
  2,
  COALESCE((
    SELECT r.backend_metadata_json
    FROM assistant_runtimes r
    WHERE r.project_id = @project_id
      AND r.assistant_id = @assistant_id
      AND r.runtime_version = 2
      AND r.backend = @backend
      AND r.backend_metadata_json <> '{}'::jsonb
    ORDER BY r.created_at DESC
    LIMIT 1
  ), '{}'::jsonb)
)
ON CONFLICT DO NOTHING;

-- name: ListAssistantPendingThreads :many
-- v2 admit needs every thread with pending events under an assistant
-- (no per-thread runtime gating — one VM serves them all). Used after
-- the v2 runtime row is reserved so the workflow can fan out to one
-- ProcessThreadEvents per thread.
SELECT t.id, t.project_id
FROM assistant_threads t
WHERE t.project_id = @project_id
  AND t.assistant_id = @assistant_id
  AND t.deleted IS FALSE
  AND EXISTS (
    SELECT 1
    FROM assistant_thread_events e
    WHERE e.project_id = t.project_id
      AND e.assistant_thread_id = t.id
      AND e.deleted IS FALSE
      AND e.status = @pending_status
  )
ORDER BY (
  SELECT MIN(e.created_at)
  FROM assistant_thread_events e
  WHERE e.project_id = t.project_id
    AND e.assistant_thread_id = t.id
    AND e.deleted IS FALSE
    AND e.status = @pending_status
) ASC;

-- name: LookupActiveAssistantRuntimeV2 :one
-- Returns the live v2 row including its state so admit can distinguish
-- starting/active (signal threads) from expiring (skip this cycle and wait
-- for the warm-timer workflow's Stop to soft-delete the row, then re-admit
-- on the next coordinator kick). The unique partial index keyed on
-- (project_id, assistant_id) means there is at most one matching row.
SELECT id, state
FROM assistant_runtimes
WHERE project_id = @project_id
  AND assistant_id = @assistant_id
  AND runtime_version = 2
  AND deleted IS FALSE
  AND ended IS FALSE
LIMIT 1;

-- name: AcquireAssistantAdvisoryLock :exec
-- pg_advisory_xact_lock auto-releases at commit. Hashed on the assistant
-- id so concurrent workers admitting the same assistant serialise on VM
-- creation; concurrent admits across assistants do not contend.
SELECT pg_advisory_xact_lock(hashtext('asst:' || @assistant_id::text));

-- name: LoadThreadContextV2 :one
-- v2 sibling of LoadThreadContext: the runtime row is keyed on assistant,
-- not thread. Joins assistant_thread → assistant → v2 runtime by
-- assistant_id, returning the same shape as LoadThreadContext (with
-- assistant_thread_id on the runtime row left NULL).
SELECT
  t.id,
  t.assistant_id,
  t.project_id,
  t.correlation_id,
  t.chat_id,
  t.source_kind,
  t.source_ref_json,
  t.last_event_at,
  a.id AS assistant_record_id,
  a.project_id AS assistant_record_project_id,
  a.organization_id,
  a.created_by_user_id,
  a.name,
  a.model,
  a.instructions,
  a.warm_ttl_seconds,
  a.max_concurrency,
  a.status,
  a.created_at,
  a.updated_at,
  a.deleted_at,
  r.id AS runtime_id,
  r.assistant_id AS runtime_assistant_id,
  r.project_id AS runtime_project_id,
  r.backend,
  r.backend_metadata_json,
  r.state,
  r.warm_until
FROM assistant_threads t
JOIN assistants a ON a.id = t.assistant_id AND a.project_id = t.project_id
JOIN assistant_runtimes r ON r.assistant_id = t.assistant_id
  AND r.project_id = t.project_id
  AND r.runtime_version = 2
WHERE t.id = @thread_id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
  AND a.deleted IS FALSE
  AND r.deleted IS FALSE
  AND r.ended IS FALSE
  AND r.state IN (@starting_state, @active_state);

-- name: TouchProcessingLease :exec
WITH touch_runtime AS (
  UPDATE assistant_runtimes r
  SET
    last_heartbeat_at = clock_timestamp(),
    updated_at = clock_timestamp()
  WHERE r.id = @runtime_id
    AND r.project_id = @project_id
    AND r.deleted IS FALSE
    AND r.state IN (@starting_state, @active_state)
)
UPDATE assistant_thread_events e
SET updated_at = clock_timestamp()
WHERE e.id = @event_id
  AND e.project_id = @project_id
  AND e.deleted IS FALSE
  AND e.status = @processing_status;

-- name: LoadThreadContext :one
SELECT
  t.id,
  t.assistant_id,
  t.project_id,
  t.correlation_id,
  t.chat_id,
  t.source_kind,
  t.source_ref_json,
  t.last_event_at,
  a.id AS assistant_record_id,
  a.project_id AS assistant_record_project_id,
  a.organization_id,
  a.created_by_user_id,
  a.name,
  a.model,
  a.instructions,
  a.warm_ttl_seconds,
  a.max_concurrency,
  a.status,
  a.created_at,
  a.updated_at,
  a.deleted_at,
  r.id AS runtime_id,
  r.assistant_thread_id,
  r.assistant_id AS runtime_assistant_id,
  r.project_id AS runtime_project_id,
  r.backend,
  r.backend_metadata_json,
  r.state,
  r.warm_until
FROM assistant_threads t
JOIN assistants a ON a.id = t.assistant_id AND a.project_id = t.project_id
JOIN assistant_runtimes r ON r.assistant_thread_id = t.id AND r.project_id = t.project_id
WHERE t.id = @thread_id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
  AND a.deleted IS FALSE
  AND r.deleted IS FALSE
  AND r.ended IS FALSE
  AND r.state IN (@starting_state, @active_state)
ORDER BY r.created_at DESC
LIMIT 1;

-- name: ClaimNextPendingEvent :one
WITH next_event AS (
  SELECT e.id
  FROM assistant_thread_events e
  WHERE e.project_id = @project_id
    AND e.assistant_thread_id = @thread_id
    AND e.deleted IS FALSE
    AND e.status = @pending_status
  ORDER BY e.created_at ASC
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
UPDATE assistant_thread_events e
SET
  status = @processing_status,
  attempts = attempts + 1,
  updated_at = clock_timestamp()
FROM next_event
WHERE e.id = next_event.id
RETURNING e.id, e.assistant_thread_id, e.assistant_id, e.project_id, e.trigger_instance_id, e.event_id, e.correlation_id, e.status, e.normalized_payload_json, e.source_payload_json, e.attempts, e.last_error;

-- name: CompleteAssistantThreadEvent :exec
UPDATE assistant_thread_events
SET
  status = @completed_status,
  processed_at = clock_timestamp(),
  last_error = NULL,
  updated_at = clock_timestamp()
WHERE id = @event_id
  AND project_id = @project_id;

-- name: FailAssistantThreadEvent :exec
UPDATE assistant_thread_events
SET
  status = @failed_status,
  last_error = @last_error,
  updated_at = clock_timestamp()
WHERE id = @event_id
  AND project_id = @project_id;

-- name: ResetAssistantThreadEventToPending :exec
UPDATE assistant_thread_events
SET
  status = @pending_status,
  last_error = @last_error,
  updated_at = clock_timestamp()
WHERE id = @event_id
  AND project_id = @project_id;

-- name: SetAssistantRuntimeActive :exec
UPDATE assistant_runtimes
SET
  state = @active_state,
  warm_until = @warm_until,
  last_heartbeat_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE id = @runtime_id
  AND project_id = @project_id;

-- name: UpdateAssistantRuntimeMetadata :exec
UPDATE assistant_runtimes
SET
  backend_metadata_json = @backend_metadata_json,
  updated_at = clock_timestamp()
WHERE id = @runtime_id
  AND project_id = @project_id;

-- name: StopAssistantRuntime :exec
UPDATE assistant_runtimes
SET
  state = @state,
  warm_until = clock_timestamp(),
  updated_at = clock_timestamp(),
  deleted_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @runtime_id
  AND deleted IS FALSE
  AND ended IS FALSE
  AND state IN (@starting_state, @active_state, @expiring_state);

-- name: ListAssistantRuntimesForReap :many
-- Returns every runtime row for an assistant that still carries backend
-- metadata, regardless of soft-delete state. A stopped row whose Fly app
-- was not collected leaves its app_name in metadata and would otherwise
-- be invisible to deleted-aware queries.
SELECT id, assistant_thread_id, assistant_id, project_id, backend, backend_metadata_json, state, warm_until
FROM assistant_runtimes
WHERE assistant_id = @assistant_id
  AND project_id = @project_id
  AND backend_metadata_json <> '{}'::jsonb;

-- name: ListInactiveAssistantRuntimesForReap :many
-- Returns runtime rows that still carry backend metadata and whose owning
-- assistant has had no runtime activity since @inactive_before. Active and
-- starting rows are excluded so a long-running session that updated_at
-- recently is never collected mid-flight.
SELECT r.id, r.assistant_thread_id, r.assistant_id, r.project_id, r.backend, r.backend_metadata_json, r.state, r.warm_until
FROM assistant_runtimes r
WHERE r.backend_metadata_json <> '{}'::jsonb
  AND r.state NOT IN (@starting_state, @active_state)
  AND NOT EXISTS (
    SELECT 1
    FROM assistant_runtimes r2
    WHERE r2.assistant_id = r.assistant_id
      AND r2.updated_at >= @inactive_before
      AND r2.backend_metadata_json <> '{}'::jsonb
  )
ORDER BY r.updated_at ASC
LIMIT @limit_count;

-- name: MarkAssistantRuntimeReaped :exec
-- Records that the backend resource (e.g. Fly app) for this runtime has
-- been torn down. Clearing backend_metadata_json removes it from the reap
-- candidate set so the janitor stops re-scanning it.
UPDATE assistant_runtimes
SET state = @reaped_state,
    backend_metadata_json = '{}'::jsonb,
    updated_at = clock_timestamp(),
    ended_at = COALESCE(ended_at, clock_timestamp()),
    deleted_at = COALESCE(deleted_at, clock_timestamp())
WHERE id = @runtime_id
  AND project_id = @project_id;

-- name: BeginExpireAssistantRuntime :one
-- Accepts both `active` and `expiring` so a Temporal-retried attempt (after
-- Stop failed mid-flight) re-enters the Status/Stop path idempotently.
-- ErrNoRows means another actor (Stop, reaper, manual API) already finalized
-- the row; callers must not then call Stop.
--
-- v2 (single-VM-per-assistant) rows pin assistant_thread_id to the admitting
-- thread, but every thread workflow under the assistant runs its own warm
-- timer and calls expire with its own thread id. Resolve the row via
-- assistant_threads.assistant_id when no v1 row matches the caller's thread
-- so any thread can flip the v2 row to expiring; the post-CAS /state poll
-- guards against tearing down a VM another thread is still using.
UPDATE assistant_runtimes
SET
  state = @expiring_state,
  updated_at = clock_timestamp()
WHERE id = (
  SELECT r.id
  FROM assistant_runtimes r
  JOIN assistant_threads t
    ON t.project_id = r.project_id
   AND t.id = @thread_id
   AND (
     r.assistant_thread_id = t.id
     OR (r.runtime_version = 2 AND r.assistant_id = t.assistant_id)
   )
  WHERE r.project_id = @project_id
    AND r.state IN (@active_state, @expiring_state)
    AND r.deleted IS FALSE
    AND r.ended IS FALSE
  ORDER BY r.runtime_version DESC, r.created_at DESC
  LIMIT 1
)
  AND project_id = @project_id
  AND state IN (@active_state, @expiring_state)
  AND deleted IS FALSE
  AND ended IS FALSE
RETURNING id, assistant_thread_id, assistant_id, project_id, backend, backend_metadata_json, state, warm_until;

-- name: RevertExpireAssistantRuntimeToActive :exec
UPDATE assistant_runtimes
SET
  state = @active_state,
  warm_until = @warm_until,
  last_heartbeat_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE id = @runtime_id
  AND project_id = @project_id
  AND state = @expiring_state;

-- name: CreateAssistantRuntime :exec
-- Inserts an assistant_runtimes row with caller-controlled id, timestamps,
-- ended_at, and deleted_at so callers can simulate stale, stuck, ended, or
-- soft-deleted runtimes. ReserveAssistantRuntime is the conflict-aware
-- production path that re-derives backend metadata from the most recent
-- runtime; this query accepts the row verbatim. Explicit id + ended_at also
-- let multiple runtime rows coexist on the same thread (the active-runtime
-- unique index ignores ended/deleted rows).
INSERT INTO assistant_runtimes (
  id,
  assistant_thread_id,
  assistant_id,
  project_id,
  backend,
  backend_metadata_json,
  state,
  warm_until,
  last_heartbeat_at,
  updated_at,
  ended_at,
  deleted_at
) VALUES (
  @id,
  @assistant_thread_id,
  @assistant_id,
  @project_id,
  @backend,
  @backend_metadata_json,
  @state,
  @warm_until,
  @last_heartbeat_at,
  @updated_at,
  @ended_at,
  @deleted_at
);

-- name: GetAssistantRuntime :one
SELECT * FROM assistant_runtimes
WHERE id = @id
  AND project_id = @project_id;

-- name: BackdateAssistantRuntimeUpdatedAt :exec
-- Test-only helper: rewinds updated_at on the active runtime for a thread so
-- backoff windows that key off updated_at can be exercised without sleeping.
UPDATE assistant_runtimes
SET updated_at = @updated_at
WHERE assistant_thread_id = @assistant_thread_id
  AND state = @state;

-- name: GetAssistantIgnoringDeleted :one
SELECT id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, status, created_at, updated_at, deleted_at
FROM assistants
WHERE id = @assistant_id
  AND project_id = @project_id;

-- name: SetAssistantStatus :exec
UPDATE assistants SET status = @status WHERE id = @id AND project_id = @project_id;

-- name: SoftDeleteAssistantThread :exec
UPDATE assistant_threads SET deleted_at = clock_timestamp() WHERE id = @id AND project_id = @project_id;

-- name: SetAssistantThreadEventStatus :exec
UPDATE assistant_thread_events
SET status = @status, updated_at = @updated_at
WHERE id = @id AND project_id = @project_id;

-- name: GetActiveAssistantRuntimeByThreadID :one
SELECT * FROM assistant_runtimes
WHERE assistant_thread_id = @assistant_thread_id
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC
LIMIT 1;

-- name: GetLatestAssistantRuntimeByThreadID :one
-- Returns the most recent runtime for a thread regardless of deletion status,
-- so callers can assert on a runtime that was just soft-deleted.
SELECT * FROM assistant_runtimes
WHERE assistant_thread_id = @assistant_thread_id
  AND project_id = @project_id
ORDER BY created_at DESC
LIMIT 1;

-- name: GetLatestAssistantThreadEventByThreadID :one
SELECT * FROM assistant_thread_events
WHERE assistant_thread_id = @assistant_thread_id
  AND project_id = @project_id
ORDER BY created_at DESC
LIMIT 1;

-- name: ReapStuckAssistantRuntimes :many
-- Short-horizon reaper for rows the happy-path can no longer move — crash
-- recovery only. Idle runtimes are collected separately through
-- ListIdleAssistantRuntimes, which must poll the runner before stopping. Both
-- branches cover rows stranded by a dead process, and the next admit
-- recreates the runtime. Stuck `expiring` v2 rows on an idle-expiring backend
-- are left to the idle sweep, which stops the backend before retiring them:
--   - 'starting' rows that never transitioned to active within the startup
--     grace window (server crashed mid-boot).
--   - 'expiring' rows older than the ExpireThreadRuntime activity's full
--     retry budget (legacy expire workflows that gave up after CAS
--     active->expiring without reaching Stop). Without this the partial
--     unique indexes (v1 on assistant_thread_id, v2 on assistant_id) block
--     new admits indefinitely.
UPDATE assistant_runtimes
SET
  state = @stopped_state,
  updated_at = clock_timestamp(),
  deleted_at = clock_timestamp()
WHERE deleted IS FALSE
  AND (
    (state = @starting_state AND updated_at < @starting_cutoff)
    OR (
      state = @expiring_state
      AND updated_at < @expiring_cutoff
      AND (runtime_version < 2 OR backend <> ALL(@idle_expiring_backends::text[]))
    )
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
  a.runtime_version AS assistant_runtime_version,
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

-- name: InitThreadSkillSnapshot :one
UPDATE assistant_threads
SET skill_set_snapshot = COALESCE(skill_set_snapshot, @candidate::jsonb)
WHERE id = @thread_id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING skill_set_snapshot;

-- name: ResolveThreadCorrelation :one
SELECT id, project_id, assistant_id, correlation_id
FROM assistant_threads
WHERE id = @thread_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ResolveThreadCorrelationByChat :one
SELECT id, project_id, assistant_id, correlation_id
FROM assistant_threads
WHERE chat_id = @chat_id
  AND assistant_id = @assistant_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetAssistantThreadIDByCorrelation :one
SELECT id
FROM assistant_threads
WHERE project_id = @project_id
  AND assistant_id = @assistant_id
  AND correlation_id = @correlation_id
  AND deleted IS FALSE;

-- name: CallerOwnsDashboardChat :one
-- Read gate for a dashboard conversation: returns a row only when the caller
-- owns the chat. The conversation key (chat id) is client-chosen and not
-- user-namespaced, so without scoping by user_id one caller could read or
-- continue another's chat. Ownership is stamped on the chats row at
-- UpsertAssistantChat time; no row means the caller has nothing here (chat
-- doesn't exist for them), which the handler surfaces as not-found so existence
-- isn't disclosed.
SELECT 1 AS ok
FROM chats
WHERE id = @chat_id
  AND project_id = @project_id
  AND user_id = @user_id
  AND deleted IS FALSE
LIMIT 1;

-- name: ResolveToolsetsForWrite :many
-- Also locks the toolsets behind canonical wrappers named by slug, so server
-- attachments take the toolset lock before the server lock, as deletion does.
SELECT t.id, t.slug, (t.mcp_enabled IS FALSE AND t.mcp_slug IS NOT NULL)::bool AS enables_mcp
FROM toolsets t
WHERE t.project_id = @project_id
  AND t.deleted IS FALSE
  AND (
    t.slug = ANY(@slugs::TEXT[])
    OR EXISTS (
      SELECT 1
      FROM mcp_servers ms
      WHERE ms.id = t.id
        AND ms.toolset_id = ms.id
        AND ms.project_id = t.project_id
        AND ms.slug = ANY(@mcp_server_slugs::TEXT[])
        AND ms.deleted IS FALSE
    )
  )
-- Serialize MCP-enable writes and deletion without blocking FK KEY SHARE
-- locks when an MCP server concurrently switches to this toolset backend.
ORDER BY t.id
FOR NO KEY UPDATE OF t;

-- name: ListAttachmentTargetIDs :many
-- Resolves the toolsets and MCP servers a write would attach so the handler
-- can authorize them first. Read-only: the write locks them itself.
SELECT t.id
FROM toolsets t
WHERE t.project_id = @project_id
  AND t.slug = ANY(@toolset_slugs::TEXT[])
  AND t.deleted IS FALSE
UNION ALL
SELECT ms.id
FROM mcp_servers ms
WHERE ms.project_id = @project_id
  AND ms.slug = ANY(@mcp_server_slugs::TEXT[])
  AND ms.deleted IS FALSE;

-- name: ResolveEnvironmentsForWrite :many
SELECT id, slug
FROM environments
WHERE project_id = @project_id
  AND slug = ANY(@slugs::TEXT[])
  AND deleted IS FALSE;

-- The active/resolvable distribution predicates in LoadAssistantSkills and
-- LoadAttachedAssistantSkill must stay identical. ResolveAssistantTurnSkills
-- intentionally omits distribution and pinning because turn-selected skills
-- resolve directly from the project registry.
-- name: LoadAssistantSkills :many
SELECT
  sd.assistant_id,
  sd.skill_id,
  sd.pinned_version_id,
  s.name,
  resolved.id AS resolved_version_id,
  resolved.description
FROM skill_distributions sd
JOIN assistants a
  ON a.id = sd.assistant_id
  AND a.project_id = sd.project_id
  AND a.deleted IS FALSE
JOIN skills s
  ON s.id = sd.skill_id
  AND s.project_id = sd.project_id
  AND s.archived_at IS NULL
JOIN LATERAL (
  SELECT sv.id, sv.description
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = sd.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE sd.assistant_id = ANY(@assistant_ids::uuid[])
  AND sd.project_id = @project_id
  AND sd.channel = 'assistant'
  AND sd.plugin_id IS NULL
  AND sd.assistant_id IS NOT NULL
  AND sd.revoked_at IS NULL
ORDER BY s.name ASC, s.id ASC;

-- name: LoadAttachedAssistantSkill :one
SELECT
  s.id AS skill_id,
  s.name,
  resolved.id AS skill_version_id,
  resolved.content,
  resolved.canonical_sha256,
  resolved.raw_sha256
FROM skill_distributions sd
JOIN assistants a
  ON a.id = sd.assistant_id
  AND a.project_id = sd.project_id
  AND a.deleted IS FALSE
JOIN skills s
  ON s.id = sd.skill_id
  AND s.project_id = sd.project_id
  AND s.archived_at IS NULL
JOIN LATERAL (
  SELECT sv.id, sv.content, sv.canonical_sha256, sv.raw_sha256
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = sd.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE sd.assistant_id = @assistant_id
  AND sd.project_id = @project_id
  AND sd.channel = 'assistant'
  AND sd.plugin_id IS NULL
  AND sd.assistant_id IS NOT NULL
  AND sd.revoked_at IS NULL
  AND s.name = @name;

-- name: ResolveAssistantTurnSkills :many
SELECT
  s.id AS skill_id,
  s.name,
  resolved.id AS resolved_version_id,
  resolved.description,
  resolved.content
FROM skills s
JOIN LATERAL (
  SELECT sv.id, sv.description, sv.content
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = s.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = s.id
    AND sv.spec_valid IS TRUE
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE s.project_id = @project_id
  AND s.id = ANY(@skill_ids::uuid[])
  AND s.archived_at IS NULL
ORDER BY s.name ASC, s.id ASC;

-- name: RecordAssistantSkillObservation :execrows
WITH observed AS (
  SELECT clock_timestamp() AS seen_at
)
INSERT INTO skill_observations (
    project_id
  , idempotency_key
  , provider
  , session_id
  , skill_name
  , raw_sha256
  , seen_at
  , skill_id
  , skill_version_id
  , reconciled_at
)
SELECT
    s.project_id
  , 'assistant:' || sqlc.arg(assistant_id)::uuid || ':' || @session_id::text || ':' || sv.id::text
  , 'assistant'
  , @session_id::text
  , s.name
  , sv.raw_sha256
  , observed.seen_at
  , s.id
  , sv.id
  , observed.seen_at
FROM skills s
JOIN skill_versions sv
  ON sv.skill_id = s.id
  AND sv.id = @skill_version_id
CROSS JOIN observed
WHERE s.project_id = @project_id
  AND s.id = @skill_id
ON CONFLICT (project_id, idempotency_key) WHERE idempotency_key IS NOT NULL
DO UPDATE SET seen_at = EXCLUDED.seen_at;

-- name: GetAssistantSkillFeedbackObservation :one
SELECT
  so.skill_id::uuid AS skill_id,
  so.skill_version_id::uuid AS skill_version_id,
  so.skill_name,
  t.chat_id
FROM assistant_threads t
JOIN skill_observations so
  ON so.project_id = t.project_id
  AND so.provider = 'assistant'
  AND so.session_id = t.chat_id::text
WHERE t.project_id = @project_id
  AND t.assistant_id = @assistant_id
  AND t.id = @thread_id
  AND t.deleted IS FALSE
  AND so.idempotency_key LIKE 'assistant:' || @assistant_id::text || ':' || t.chat_id::text || ':%'
  AND so.skill_name = @skill_name
  AND so.reconciled_at IS NOT NULL
  AND so.reconcile_error_code IS NULL
ORDER BY so.seen_at DESC, so.id DESC
LIMIT 1;

-- name: EnableMCPForToolsets :many
-- Flips mcp_enabled to TRUE for the listed toolsets in a project. Newly
-- attached toolsets are enabled so users don't have to do it separately; the
-- caller picks them by ResolveToolsetsForWrite's enables_mcp under its lock.
UPDATE toolsets
SET mcp_enabled = TRUE,
    updated_at = clock_timestamp()
WHERE id = ANY(@toolset_ids::UUID[])
  AND project_id = @project_id
  AND mcp_enabled IS FALSE
  AND mcp_slug IS NOT NULL
  AND deleted IS FALSE
RETURNING id;

-- name: LoadAssistantMcpServers :many
-- Hosted rows (canonical wrapper, id = toolset id) keep the toolset slug,
-- mcp_enabled, /mcp/{mcp_slug} address and default environment, and sort first
-- so they win runtime id collisions.
SELECT
  ams.assistant_id,
  ams.mcp_server_id,
  ms.slug AS server_slug,
  t.slug AS toolset_slug,
  CASE WHEN t.id IS NOT NULL THEN CASE
    WHEN t.mcp_enabled IS NOT TRUE THEN 'disabled'
    WHEN t.mcp_is_public THEN 'public'
    ELSE 'private'
  END ELSE ms.visibility END::text AS visibility,
  CASE WHEN t.id IS NOT NULL THEN COALESCE(t.mcp_slug, '') ELSE COALESCE((
    SELECT me.slug
    FROM mcp_endpoints me
    WHERE me.mcp_server_id = ms.id
      AND me.project_id = ms.project_id
      AND me.custom_domain_id IS NULL
      AND me.deleted IS FALSE
    ORDER BY me.created_at
    LIMIT 1
  ), '') END::text AS endpoint_slug,
  t.default_environment_slug,
  ams.environment_id,
  e.slug AS environment_slug
FROM assistant_mcp_servers ams
JOIN mcp_servers ms
  ON ms.id = ams.mcp_server_id
  AND ms.project_id = @project_id
  AND ms.deleted IS FALSE
LEFT JOIN toolsets t
  ON t.id = ms.id
  AND ms.toolset_id = ms.id
  AND t.project_id = @project_id
  AND t.deleted IS FALSE
LEFT JOIN environments e ON e.id = ams.environment_id AND e.project_id = @project_id
WHERE ams.assistant_id = ANY(@assistant_ids::UUID[])
  AND ams.project_id = @project_id
ORDER BY (t.id IS NULL), ams.created_at, ams.mcp_server_id;

-- name: ClearAssistantHostedMcpServers :exec
-- Detaches the assistant's toolset-backed (canonical wrapper) servers only.
DELETE FROM assistant_mcp_servers ams
USING mcp_servers ms
WHERE ams.mcp_server_id = ms.id
  AND ms.project_id = ams.project_id
  AND ms.toolset_id = ms.id
  AND ams.assistant_id = @assistant_id
  AND ams.project_id = @project_id;

-- name: ClearAssistantDirectMcpServers :exec
-- Detaches the assistant's servers that are not canonical hosted wrappers.
DELETE FROM assistant_mcp_servers ams
WHERE ams.assistant_id = @assistant_id
  AND ams.project_id = @project_id
  AND NOT EXISTS (
    SELECT 1
    FROM mcp_servers ms
    WHERE ms.id = ams.mcp_server_id
      AND ms.project_id = ams.project_id
      AND ms.toolset_id = ms.id
  );

-- name: ListAssistantAttachedServerIDs :many
SELECT ams.mcp_server_id
FROM assistant_mcp_servers ams
WHERE ams.assistant_id = @assistant_id
  AND ams.project_id = @project_id;

-- name: ListCanonicalWrapperIDs :many
-- Writer-only: of the given toolsets, those with a live canonical wrapper (same id); hosted attachments are stored under it.
SELECT ms.id
FROM mcp_servers ms
JOIN toolsets t ON t.id = ms.id AND t.project_id = ms.project_id
WHERE ms.project_id = @project_id
  AND ms.id = ANY(@toolset_ids::UUID[])
  AND ms.toolset_id = ms.id
  AND ms.deleted IS FALSE
  AND t.deleted IS FALSE;

-- name: ClearAssistantMcpServersForServers :exec
DELETE FROM assistant_mcp_servers
WHERE assistant_id = @assistant_id
  AND project_id = @project_id
  AND mcp_server_id = ANY(@mcp_server_ids::UUID[]);

-- name: ResolveMcpServersForWrite :many
-- Besides resolving slugs to ids, this returns everything attach-time
-- validation needs to reject servers the assistant runtime cannot reach:
-- the backend kind (tunnelled servers have no serving path), visibility,
-- and whether a Speakeasy-hosted endpoint exists to build the /mcp/{slug} URL.
-- A canonical hosted wrapper may use its toolset's legacy mcp_slug address.
SELECT
  ms.id,
  ms.slug,
  ms.visibility,
  (ms.tunneled_mcp_server_id IS NOT NULL)::bool AS tunneled,
  (ms.toolset_id IS NOT NULL AND ms.toolset_id = ms.id)::bool AS hosted,
  (EXISTS (
    SELECT 1
    FROM mcp_endpoints me
    WHERE me.mcp_server_id = ms.id
      AND me.project_id = ms.project_id
      AND me.custom_domain_id IS NULL
      AND me.deleted IS FALSE
  ) OR (ms.toolset_id = ms.id AND EXISTS (
    SELECT 1
    FROM toolsets t
    WHERE t.id = ms.toolset_id
      AND t.project_id = ms.project_id
      AND t.mcp_slug IS NOT NULL
      AND t.deleted IS FALSE
  )))::bool AS has_gram_endpoint
FROM mcp_servers ms
WHERE ms.project_id = @project_id
  AND ms.slug = ANY(@slugs::TEXT[])
  AND ms.deleted IS FALSE
ORDER BY ms.id
FOR NO KEY UPDATE OF ms;

-- name: AddAssistantMcpServers :copyfrom
INSERT INTO assistant_mcp_servers (
  assistant_id,
  mcp_server_id,
  environment_id,
  project_id
) VALUES (
  @assistant_id,
  @mcp_server_id,
  @environment_id,
  @project_id
);

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
RETURNING id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, runtime_version, status, created_at, updated_at, deleted_at;

-- name: ListAssistants :many
SELECT id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, runtime_version, status, created_at, updated_at, deleted_at
FROM assistants
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: GetAssistant :one
SELECT id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, runtime_version, status, created_at, updated_at, deleted_at
FROM assistants
WHERE id = @assistant_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetAssistantForDispatch :one
SELECT id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, runtime_version, status, created_at, updated_at, deleted_at
FROM assistants
WHERE id = @assistant_id
  AND deleted IS FALSE;

-- name: GetManagedAssistantByProject :one
-- Resolves a project's platform-managed assistant (powers the AI Insights
-- sidebar) through the project_managed_assistants mapping. Returns no rows
-- when the feature isn't toggled on for the project.
SELECT a.id, a.project_id, a.organization_id, a.created_by_user_id, a.name, a.model, a.instructions, a.warm_ttl_seconds, a.max_concurrency, a.runtime_version, a.status, a.created_at, a.updated_at, a.deleted_at
FROM project_managed_assistants pma
JOIN assistants a ON a.id = pma.assistant_id
WHERE pma.project_id = @project_id
  AND a.deleted IS FALSE;

-- name: GetProjectName :one
-- Display name of a project, used to compose the managed assistant's name so
-- it's distinguishable from other projects' managed assistants in the same org.
SELECT name
FROM projects
WHERE id = @project_id
  AND deleted IS FALSE;

-- name: CreateProjectManagedAssistant :exec
-- Marks an assistant as the project's managed assistant. PRIMARY KEY(project_id)
-- enforces 0-or-1, so a concurrent enable raises a unique violation the caller
-- recovers from by re-reading.
INSERT INTO project_managed_assistants (project_id, assistant_id)
VALUES (@project_id, @assistant_id);

-- name: DeleteProjectManagedAssistant :exec
-- Toggles the managed assistant off for a project (the assistant row itself is
-- soft-deleted separately). Returns silently if no mapping exists.
DELETE FROM project_managed_assistants
WHERE project_id = @project_id;

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
RETURNING id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, runtime_version, status, created_at, updated_at, deleted_at;

-- name: DeleteAssistant :exec
UPDATE assistants
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @assistant_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: RetireAssistantMCPOAuthClients :exec
UPDATE assistant_mcp_oauth_clients
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE assistant_id = @assistant_id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: RevokeSkillDistributionsByAssistant :many
-- Returns pre-revocation state and skill identity for per-edge audit events.
UPDATE skill_distributions sd
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM skill_distributions prev
JOIN skills s ON s.id = prev.skill_id AND s.project_id = prev.project_id
JOIN assistants a ON a.id = prev.assistant_id AND a.project_id = prev.project_id
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = prev.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = prev.skill_id
    AND sv.spec_valid IS TRUE
    AND (prev.pinned_version_id IS NULL OR sv.id = prev.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE prev.id = sd.id
  AND sd.project_id = @project_id
  AND sd.assistant_id = @assistant_id
  AND sd.channel = 'assistant'
  AND sd.plugin_id IS NULL
  AND sd.revoked_at IS NULL
RETURNING sd.*, prev.updated_at AS previous_updated_at, resolved.id AS resolved_version_id,
  s.name AS skill_name, s.display_name AS skill_display_name, a.organization_id;

-- name: UpsertAssistantChat :exec
-- user_id is the conversation owner — stamped on first insert so reads can
-- scope to the user who started the chat. The dashboard source passes the
-- Speakeasy user id; external-source turns (Slack/cron/wake) pass NULL. On conflict
-- the existing user_id is preserved when already set so a later NULL-user-id
-- retry doesn't unclaim the chat; pre-existing rows with NULL user_id are
-- backfilled on first owned send so dashboard ownership checks accept the
-- legitimate owner.
INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id, title, created_at, updated_at)
VALUES (@chat_id, @project_id, @organization_id, sqlc.narg('user_id')::TEXT, NULL, @title, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
  user_id = COALESCE(chats.user_id, EXCLUDED.user_id),
  updated_at = NOW();

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
WITH ingestion_fence AS MATERIALIZED (
  SELECT pg_advisory_xact_lock(hashtext('asst:' || sqlc.arg('assistant_id')::uuid::text))
)
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
) SELECT
  @assistant_thread_id,
  @assistant_id,
  @project_id,
  @trigger_instance_id,
  @event_id,
  @correlation_id,
  @status,
  @normalized_payload_json,
  @source_payload_json
FROM ingestion_fence
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

-- name: CountActiveAssistantThreads :one
-- Threads with last_event_at inside the warm TTL window. Excludes threads
-- that themselves have a pending event so callers computing headroom for
-- a fresh pending admit don't double-count it. The event-less warmup
-- thread is excluded too: it holds the VM warm but never occupies a
-- runner slot, and counting it would block max_concurrency=1 assistants
-- from admitting their first real turn until the window lapses.
-- Client-driven setup/onboarding threads are excluded for the same reason:
-- they carry no runtime events and never occupy a runner slot, so counting
-- them would wrongly consume max_concurrency / warm headroom against real
-- turns even though the setup chat runs entirely client-side.
SELECT COUNT(*)::BIGINT AS active_threads
FROM assistant_threads t
WHERE t.project_id = @project_id
  AND t.assistant_id = @assistant_id
  AND t.deleted IS FALSE
  AND t.source_kind <> @warmup_source_kind
  AND t.source_kind <> @setup_source_kind
  AND t.last_event_at > @active_since
  AND NOT EXISTS (
    SELECT 1
    FROM assistant_thread_events e
    WHERE e.project_id = t.project_id
      AND e.assistant_thread_id = t.id
      AND e.deleted IS FALSE
      AND e.status = @pending_status
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
-- a real reference. The persisted desired version is stamped once at creation;
-- it is never changed on a runtime row. Versions 2 and 3 share the unique index.
-- The unique partial index
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
  (SELECT a.runtime_version FROM assistants a WHERE a.id = @assistant_id AND a.project_id = @project_id AND a.deleted IS FALSE),
  COALESCE((
    SELECT r.backend_metadata_json
    FROM assistant_runtimes r
    WHERE r.project_id = @project_id
      AND r.assistant_id = @assistant_id
      -- Only legacy v2 bindings may survive runtime-row replacement. Kit v3
      -- metadata carries immutable runtime-row authority and must start empty.
      AND r.runtime_version = 2
      AND r.runtime_version = (SELECT a.runtime_version FROM assistants a WHERE a.id = @assistant_id AND a.project_id = @project_id)
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
  AND runtime_version IN (2, 3)
  AND deleted IS FALSE
  AND ended IS FALSE
LIMIT 1;

-- name: GetAssistantRuntimeV2 :one
-- Full-row sibling of LookupActiveAssistantRuntimeV2 for callers that need
-- the backend metadata to drive Ensure without a thread to join through
-- (eager warmup at assistant creation). At most one row matches per the
-- v2 unique partial index.
SELECT id, assistant_thread_id, assistant_id, project_id, runtime_version, backend, backend_metadata_json, state, warm_until
FROM assistant_runtimes
WHERE project_id = @project_id
  AND assistant_id = @assistant_id
  AND runtime_version IN (2, 3)
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
  a.runtime_version AS assistant_runtime_version,
  a.status,
  a.created_at,
  a.updated_at,
  a.deleted_at,
  r.id AS runtime_id,
  r.assistant_id AS runtime_assistant_id,
  r.project_id AS runtime_project_id,
  r.runtime_version,
  r.backend,
  r.backend_metadata_json,
  r.state,
  r.warm_until
FROM assistant_threads t
JOIN assistants a ON a.id = t.assistant_id AND a.project_id = t.project_id
JOIN assistant_runtimes r ON r.assistant_id = t.assistant_id
  AND r.project_id = t.project_id
  AND r.runtime_version IN (2, 3)
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
  a.runtime_version AS assistant_runtime_version,
  a.status,
  a.created_at,
  a.updated_at,
  a.deleted_at,
  r.id AS runtime_id,
  r.assistant_thread_id,
  r.assistant_id AS runtime_assistant_id,
  r.project_id AS runtime_project_id,
  r.runtime_version,
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
  SELECT e.id, t.skill_set_snapshot
  FROM assistant_thread_events e
  JOIN assistant_threads t
    ON t.id = e.assistant_thread_id
    AND t.project_id = e.project_id
  WHERE e.project_id = @project_id
    AND e.assistant_thread_id = @thread_id
    AND e.deleted IS FALSE
    AND e.status = @pending_status
    AND t.deleted IS FALSE
  ORDER BY e.created_at ASC
  LIMIT 1
  FOR UPDATE OF e SKIP LOCKED
)
UPDATE assistant_thread_events e
SET
  status = @processing_status,
  attempts = attempts + 1,
  updated_at = clock_timestamp()
FROM next_event
WHERE e.id = next_event.id
  AND e.project_id = @project_id
RETURNING e.id, e.assistant_thread_id, e.assistant_id, e.project_id, e.trigger_instance_id, e.event_id, e.correlation_id, e.status, e.normalized_payload_json, e.source_payload_json, e.attempts, e.last_error, e.created_at, next_event.skill_set_snapshot;

-- name: CompleteAssistantThreadEventAndAdvanceSkillSnapshot :one
WITH completed_event AS (
  UPDATE assistant_thread_events event
  SET
    status = @completed_status,
    processed_at = clock_timestamp(),
    last_error = NULL,
    updated_at = clock_timestamp()
  WHERE event.id = @event_id
    AND event.project_id = @project_id
    AND event.status = @processing_status
    AND event.attempts = @claimed_attempt
  RETURNING event.assistant_thread_id
), advanced_snapshot AS (
  UPDATE assistant_threads t
  SET skill_set_snapshot = sqlc.narg('current_snapshot')::jsonb
  FROM completed_event e
  WHERE t.id = e.assistant_thread_id
    AND t.project_id = @project_id
    AND t.deleted IS FALSE
    AND t.skill_set_snapshot IS NOT DISTINCT FROM sqlc.narg('claimed_snapshot')::jsonb
    AND (t.skill_set_snapshot IS NULL OR @allow_advance::boolean)
    AND (
      sqlc.narg('claimed_snapshot')::jsonb IS NULL
      OR sqlc.narg('current_snapshot')::jsonb IS DISTINCT FROM sqlc.narg('claimed_snapshot')::jsonb
    )
  RETURNING t.id
)
SELECT EXISTS(SELECT 1 FROM completed_event) AS completed;

-- name: FailAssistantThreadEvent :exec
UPDATE assistant_thread_events
SET
  status = @failed_status,
  last_error = @last_error,
  updated_at = clock_timestamp()
WHERE id = @event_id
  AND project_id = @project_id;

-- name: CancelPendingAssistantThreadEvents :execrows
-- Drops every turn queued on a thread that no runner has claimed yet. Used by
-- the stop button: a turn still waiting on a cold runtime has no in-flight
-- generation to cancel, so unless it is taken out of the queue here it starts
-- generating moments after the user asked it not to. Claimed ('processing')
-- events are deliberately untouched — those are already dispatched, and the
-- runner's own interrupt is what stops them.
UPDATE assistant_thread_events
SET
  status = @cancelled_status,
  processed_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND assistant_thread_id = @thread_id
  AND status = @pending_status
  AND deleted IS FALSE;

-- name: ResetAssistantThreadEventToPending :exec
WITH ingestion_fence AS MATERIALIZED (
  SELECT e.id, pg_advisory_xact_lock(hashtext('asst:' || e.assistant_id::text))
  FROM assistant_thread_events e
  WHERE e.id = @event_id AND e.project_id = @project_id
)
UPDATE assistant_thread_events e
SET
  status = @pending_status,
  attempts = GREATEST(0, attempts - CASE WHEN @restore_attempt::boolean THEN 1 ELSE 0 END),
  last_error = @last_error,
  updated_at = clock_timestamp()
FROM ingestion_fence
WHERE e.id = ingestion_fence.id
  AND e.project_id = @project_id
  AND e.status = 'processing';

-- name: SetAssistantRuntimeActive :exec
UPDATE assistant_runtimes
SET
  state = @active_state,
  warm_until = @warm_until,
  last_heartbeat_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE id = @runtime_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND ended IS FALSE;

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
-- metadata, regardless of soft-delete state. A stopped row whose resources
-- were not collected retains backend metadata and would otherwise
-- be invisible to deleted-aware queries.
SELECT id, assistant_thread_id, assistant_id, project_id, runtime_version, backend, backend_metadata_json, state, warm_until
FROM assistant_runtimes
WHERE assistant_id = @assistant_id
  AND project_id = @project_id
  AND backend_metadata_json <> '{}'::jsonb;

-- name: ListInactiveAssistantRuntimesForReap :many
-- Returns runtime rows that still carry backend metadata and are safe to
-- collect, in two cases:
--   1. Orphans: finalized (soft-deleted or ended; the state value is ignored
--      since a tombstone can carry any state, e.g. one stamped by a racing
--      turn) or under a soft-deleted assistant, whose owning assistant has had
--      no runtime activity since @inactive_before — i.e. Stop/Reap never
--      completed. A live row under a live assistant is never an orphan: an
--      idle runtime keeps its VM until the assistant is deleted.
--   2. Superseded backend: a row on a backend the process no longer targets
--      (backend <> @target_backend), parked at @stopped_state after its warm
--      window. Collecting it frees the deprecated backend's resources so the
--      next admit lands on the target backend. Active/starting/expiring rows
--      are left to finish, so a live assistant is never disrupted mid-turn.
SELECT r.id, r.assistant_thread_id, r.assistant_id, r.project_id, r.runtime_version, r.backend, r.backend_metadata_json, r.state, r.warm_until
FROM assistant_runtimes r
WHERE r.backend_metadata_json <> '{}'::jsonb
  AND (
    (
      (
        r.deleted IS TRUE
        OR r.ended IS TRUE
        OR EXISTS (
          SELECT 1
          FROM assistants a
          WHERE a.id = r.assistant_id
            AND a.project_id = r.project_id
            AND a.deleted IS TRUE
        )
      )
      AND NOT EXISTS (
        SELECT 1
        FROM assistant_runtimes r2
        WHERE r2.assistant_id = r.assistant_id
          AND r2.updated_at >= @inactive_before
          AND r2.backend_metadata_json <> '{}'::jsonb
      )
    )
    OR (
      r.backend <> @target_backend
      AND r.deleted IS NOT TRUE
      AND r.ended IS NOT TRUE
      AND r.state = @stopped_state
    )
  )
ORDER BY r.updated_at ASC
LIMIT @limit_count;

-- name: CountInFlightAssistantThreadEvents :one
-- Counts events that are queued for or currently using the assistant's
-- runtime. The image recycle sweep skips assistants with any in-flight
-- events: their turns are about to hit the VM (the runner's idle clock
-- only clears on /turn enqueue) and their admissions recycle the image
-- lazily anyway.
SELECT COUNT(*)
FROM assistant_thread_events
WHERE project_id = @project_id
  AND assistant_id = @assistant_id
  AND deleted IS FALSE
  AND status IN (@pending_status, @processing_status);

-- name: UpdateActiveAssistantRuntimeMetadata :execrows
-- Persists post-recycle backend metadata only while the row is still live.
-- Zero rows affected means the warm timer expired the runtime mid-recycle;
-- the caller must undo the machine restart instead of recording it.
UPDATE assistant_runtimes
SET
  backend_metadata_json = @backend_metadata_json,
  updated_at = clock_timestamp()
WHERE id = @runtime_id
  AND project_id = @project_id
  AND state = @active_state
  AND deleted IS FALSE;

-- name: ListActiveAssistantRuntimes :many
-- Returns live v2 runtime rows that carry backend metadata for runtime
-- reconciliation sweeps. Only `active` rows qualify: `starting` rows are
-- mid-boot, while expiring/stopped rows are already being torn down or await
-- re-admission. Rows orphaned by a deleted assistant belong to the
-- deleted-assistant janitor and are excluded here.
SELECT r.id, r.assistant_thread_id, r.assistant_id, r.project_id, r.runtime_version, r.backend, r.backend_metadata_json, r.state, r.warm_until
FROM assistant_runtimes r
WHERE r.state = @active_state
  AND r.runtime_version IN (2, 3)
  AND r.deleted IS FALSE
  AND r.ended IS FALSE
  AND r.backend_metadata_json <> '{}'::jsonb
  AND NOT EXISTS (
    SELECT 1
    FROM assistants a
    WHERE a.id = r.assistant_id
      AND a.project_id = r.project_id
      AND a.deleted IS TRUE
  )
ORDER BY r.updated_at ASC;

-- name: MarkAssistantRuntimeReaped :exec
-- Records that the backend resource (e.g. sandbox claim) for this runtime has
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

-- name: ListStoppedRuntimesForReap :many
-- Per-thread reap candidates: rows in stopped or failed whose own updated_at
-- has aged past @stopped_before. No assistant-wide activity check so a single
-- still-active thread on an assistant cannot pin every dead sibling's machine
-- indefinitely. The NOT EXISTS guard restricts the candidate to the most
-- recent non-reaped row per thread: ReserveAssistantRuntime copies the latest
-- non-empty metadata into each new row, so a stale older sibling can share
-- resource metadata with the current owner — destroying it would yank the live
-- machine out from under a fresher row that is still inside its own TTL.
SELECT r.id, r.assistant_thread_id, r.assistant_id, r.project_id, r.runtime_version, r.backend, r.backend_metadata_json, r.state, r.warm_until
FROM assistant_runtimes r
WHERE r.backend_metadata_json <> '{}'::jsonb
  AND r.state IN (@stopped_state, @failed_state)
  AND r.updated_at < @stopped_before
  AND NOT EXISTS (
    SELECT 1
    FROM assistant_runtimes r2
    WHERE r2.assistant_thread_id = r.assistant_thread_id
      AND r2.project_id = r.project_id
      AND r2.created_at > r.created_at
      AND r2.state <> @reaped_state
  )
ORDER BY r.updated_at ASC
LIMIT @limit_count;

-- name: MarkAssistantRuntimeMachineReaped :execrows
-- Per-thread reap: marks the stopped resource reaped while retaining backend
-- metadata for full cleanup (e.g. local workspace volumes). Filters by current
-- state so a row that raced into starting/active between selection and
-- update is left alone.
UPDATE assistant_runtimes
SET state = @reaped_state,
    updated_at = clock_timestamp(),
    ended_at = COALESCE(ended_at, clock_timestamp()),
    deleted_at = COALESCE(deleted_at, clock_timestamp())
WHERE id = @runtime_id
  AND project_id = @project_id
  AND state IN (@stopped_state, @failed_state);

-- name: BeginExpireAssistantRuntime :one
-- Accepts both `active` and `expiring` so a Temporal-retried attempt (after
-- Stop failed mid-flight) re-enters the Status/Stop path idempotently.
-- ErrNoRows means another actor (Stop, reaper, manual API) already finalized
-- the row; callers must not then call Stop.
--
-- Only rows on an idle-expiring backend match; thread workflows on any other
-- backend exit the expiry loop and leave the runtime up.
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
     OR (r.runtime_version IN (2, 3) AND r.assistant_id = t.assistant_id)
   )
  WHERE r.project_id = @project_id
    AND r.state IN (@active_state, @expiring_state)
    AND r.deleted IS FALSE
    AND r.ended IS FALSE
    AND r.backend = ANY(@idle_expiring_backends::text[])
  ORDER BY r.runtime_version DESC, r.created_at DESC
  LIMIT 1
)
  AND project_id = @project_id
  AND state IN (@active_state, @expiring_state)
  AND deleted IS FALSE
  AND ended IS FALSE
RETURNING id, assistant_thread_id, assistant_id, project_id, runtime_version, backend, backend_metadata_json, state, warm_until;

-- name: ListIdleAssistantRuntimes :many
-- Idle-sweep candidates on an idle-expiring backend:
--   - `active` v2 rows whose warm window lapsed before @warm_until_before, with
--     no thread workflow left to expire them.
--   - `expiring` rows untouched since @expiring_before, whose expire gave up
--     before stopping the backend.
-- Assistants with a turn in flight are skipped: warm_until only advances when
-- a turn ends, so a long turn can outlive its window.
SELECT r.id, r.project_id, a.warm_ttl_seconds
FROM assistant_runtimes r
JOIN assistants a
  ON a.id = r.assistant_id
 AND a.project_id = r.project_id
 AND a.deleted IS FALSE
WHERE r.runtime_version IN (2, 3)
  AND r.deleted IS FALSE
  AND r.ended IS FALSE
  AND r.backend = ANY(@idle_expiring_backends::text[])
  AND (
    (r.state = @active_state AND r.warm_until < @warm_until_before)
    OR (r.state = @expiring_state AND r.updated_at < @expiring_before)
  )
  AND NOT EXISTS (
    SELECT 1
    FROM assistant_thread_events e
    WHERE e.assistant_id = r.assistant_id
      AND e.project_id = r.project_id
      AND e.deleted IS FALSE
      AND e.status = @processing_status
  )
ORDER BY r.updated_at ASC
LIMIT @max_rows;

-- name: BeginExpireIdleAssistantRuntime :one
-- Runtime-id counterpart of BeginExpireAssistantRuntime for the reaper's idle
-- sweep. Takes `active` rows, and `expiring` rows untouched since
-- @expiring_before so a stalled expire is retried rather than left holding the
-- v2 unique index. ErrNoRows means another actor moved the row first.
UPDATE assistant_runtimes
SET
  state = @expiring_state,
  updated_at = clock_timestamp()
WHERE id = @runtime_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND ended IS FALSE
  AND (
    state = @active_state
    OR (state = @expiring_state AND updated_at < @expiring_before)
  )
RETURNING id, assistant_thread_id, assistant_id, project_id, runtime_version, backend, backend_metadata_json, state, warm_until;

-- name: RevertExpireAssistantRuntimeToActive :exec
UPDATE assistant_runtimes
SET
  state = @active_state,
  warm_until = @warm_until,
  last_heartbeat_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE id = @runtime_id
  AND project_id = @project_id
  AND state = @expiring_state
  AND deleted IS FALSE
  AND ended IS FALSE;

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

-- name: SetAssistantRuntimeStateForTest :exec
-- Test fixture: forces a runtime row into a state with a chosen updated_at so
-- tests can simulate an expire that stalled long ago.
UPDATE assistant_runtimes
SET
  state = @state,
  updated_at = @updated_at
WHERE id = @runtime_id
  AND project_id = @project_id;

-- name: GetAssistantRuntime :one
SELECT * FROM assistant_runtimes
WHERE id = @id
  AND project_id = @project_id;

-- name: GetAssistantRuntimeForAdmission :one
SELECT * FROM assistant_runtimes
WHERE id = @id AND project_id = @project_id
FOR UPDATE;

-- name: BackdateAssistantRuntimeUpdatedAt :exec
-- Test-only helper: rewinds updated_at on the active runtime for a thread so
-- backoff windows that key off updated_at can be exercised without sleeping.
UPDATE assistant_runtimes
SET updated_at = @updated_at
WHERE assistant_thread_id = @assistant_thread_id
  AND state = @state;

-- name: GetAssistantIgnoringDeleted :one
SELECT id, project_id, organization_id, created_by_user_id, name, model, instructions, warm_ttl_seconds, max_concurrency, runtime_version, status, created_at, updated_at, deleted_at
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

-- name: GetAssistantMCPOAuthClient :one
SELECT
  client_id,
  client_secret_encrypted,
  client_id_metadata_uri,
  (
    client_id IS NOT NULL
    AND redirect_uri = @redirect_uri
    AND (client_secret_expires_at IS NULL OR client_secret_expires_at > @usable_after)
    AND (
      client_secret_encrypted IS NOT NULL
      OR client_id_metadata_uri IS NOT NULL
    )
  ) AS usable,
  (
    client_id IS NOT NULL
    AND client_secret_expires_at IS NOT NULL
    AND client_secret_expires_at <= @usable_after
  ) AS invalidated,
  (
    (
      client_id IS NULL
      AND registration_started_at < clock_timestamp() - @claim_lease::interval
    )
    OR
    (
      client_id IS NOT NULL
      AND client_secret_expires_at IS NOT NULL
      AND client_secret_expires_at <= @usable_after
    )
    OR (client_id IS NOT NULL AND redirect_uri <> @redirect_uri)
  ) AS claimable
FROM assistant_mcp_oauth_clients clients
WHERE clients.project_id = @project_id
  AND clients.assistant_id = @assistant_id
  AND clients.oauth_server_issuer = @oauth_server_issuer
  AND EXISTS (
    SELECT 1
    FROM assistants owner
    WHERE owner.id = @assistant_id
      AND owner.project_id = @project_id
      AND owner.deleted IS FALSE
  )
  AND clients.deleted IS FALSE;

-- name: ClaimAssistantMCPOAuthClientRegistration :execrows
INSERT INTO assistant_mcp_oauth_clients AS clients (
  project_id,
  assistant_id,
  oauth_server_issuer,
  redirect_uri,
  registration_owner,
  registration_started_at
) SELECT
  @project_id,
  @assistant_id,
  @oauth_server_issuer,
  @redirect_uri,
  @registration_owner,
  clock_timestamp()
FROM assistants owner
WHERE owner.id = @assistant_id
  AND owner.project_id = @project_id
  AND owner.deleted IS FALSE
FOR UPDATE
ON CONFLICT (project_id, assistant_id, oauth_server_issuer) WHERE deleted IS FALSE
DO UPDATE SET
  client_id = NULL,
  client_secret_encrypted = NULL,
  client_secret_expires_at = NULL,
  client_id_metadata_uri = NULL,
  redirect_uri = EXCLUDED.redirect_uri,
  registration_owner = EXCLUDED.registration_owner,
  registration_started_at = EXCLUDED.registration_started_at,
  updated_at = clock_timestamp()
WHERE
  (
    clients.client_id IS NULL
    AND clients.registration_started_at < clock_timestamp() - @claim_lease::interval
  )
  OR
  (
    clients.client_id IS NOT NULL
    AND clients.client_secret_expires_at IS NOT NULL
    AND clients.client_secret_expires_at <= @usable_after
  )
  OR (
    clients.client_id IS NOT NULL
    AND clients.redirect_uri <> EXCLUDED.redirect_uri
  );

-- name: CompleteAssistantMCPOAuthClientRegistration :execrows
UPDATE assistant_mcp_oauth_clients AS clients
SET
  client_id = @client_id,
  client_secret_encrypted = @client_secret_encrypted,
  client_secret_expires_at = @client_secret_expires_at,
  client_id_metadata_uri = NULL,
  registration_owner = NULL,
  registration_started_at = NULL,
  updated_at = clock_timestamp()
WHERE clients.project_id = @project_id
  AND clients.assistant_id = @assistant_id
  AND clients.oauth_server_issuer = @oauth_server_issuer
  AND clients.registration_owner = @registration_owner
  AND clients.client_id IS NULL
  AND EXISTS (
    SELECT 1
    FROM assistants owner
    WHERE owner.id = @assistant_id
      AND owner.project_id = @project_id
      AND owner.deleted IS FALSE
  )
  AND clients.deleted IS FALSE;

-- name: AbandonAssistantMCPOAuthClientRegistration :exec
UPDATE assistant_mcp_oauth_clients
SET
  registration_started_at = to_timestamp(0),
  updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND assistant_id = @assistant_id
  AND oauth_server_issuer = @oauth_server_issuer
  AND registration_owner = @registration_owner
  AND client_id IS NULL
  AND deleted IS FALSE;

-- name: InvalidateAssistantMCPOAuthClient :exec
UPDATE assistant_mcp_oauth_clients
SET
  client_secret_expires_at = to_timestamp(0),
  updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND assistant_id = @assistant_id
  AND oauth_server_issuer = @oauth_server_issuer
  AND client_id = @client_id
  AND client_id IS NOT NULL
  AND deleted IS FALSE;

-- name: UpsertAssistantMCPOAuthClientCIMD :one
-- Records a public CIMD client whose client_id is the document URL. Does not
-- replace a live confidential DCR registration or an in-progress claim: that
-- reuse path stays on the secret-bearing row until it expires, is
-- invalidated, or was registered for another redirect. Intentionally
-- project-scoped.
INSERT INTO assistant_mcp_oauth_clients AS clients (
  project_id,
  assistant_id,
  oauth_server_issuer,
  redirect_uri,
  client_id,
  client_id_metadata_uri
) SELECT
  @project_id,
  @assistant_id,
  @oauth_server_issuer,
  @redirect_uri,
  @client_id,
  @client_id_metadata_uri
FROM assistants owner
WHERE owner.id = @assistant_id
  AND owner.project_id = @project_id
  AND owner.deleted IS FALSE
ON CONFLICT (project_id, assistant_id, oauth_server_issuer) WHERE deleted IS FALSE
DO UPDATE SET
  redirect_uri = EXCLUDED.redirect_uri,
  client_id = EXCLUDED.client_id,
  client_id_metadata_uri = EXCLUDED.client_id_metadata_uri,
  client_secret_encrypted = NULL,
  client_secret_expires_at = NULL,
  registration_owner = NULL,
  registration_started_at = NULL,
  updated_at = clock_timestamp()
WHERE
  clients.client_id_metadata_uri IS NOT NULL
  OR (
    clients.client_id IS NULL
    AND clients.registration_started_at < clock_timestamp() - @claim_lease::interval
  )
  OR (
    clients.client_id IS NOT NULL
    AND clients.client_secret_expires_at IS NOT NULL
    AND clients.client_secret_expires_at <= @usable_after
  )
  OR (
    clients.client_id IS NOT NULL
    AND clients.redirect_uri <> EXCLUDED.redirect_uri
  )
RETURNING client_id, client_secret_encrypted, client_id_metadata_uri;

-- name: GetAssistantForClientMetadataDocument :one
-- Public CIMD document endpoint lookup. Intentionally NOT project-scoped: the
-- endpoint is unauthenticated and addresses assistants by their globally
-- unique primary key. The served document exposes the assistant's display
-- name, dashboard URI, and redirect_uri — the same identity sent upstream as
-- client_id. A deleted assistant or missing org/project yields no row, so
-- the handler 404s.
SELECT
  a.id,
  a.project_id,
  a.name,
  p.slug AS project_slug,
  om.slug AS organization_slug
FROM assistants a
JOIN projects p ON p.id = a.project_id AND p.deleted IS FALSE
JOIN organization_metadata om ON om.id = a.organization_id
WHERE a.id = @assistant_id
  AND a.deleted IS FALSE;

-- name: GetAssistantMCPOAuthClientDeleted :one
-- Test-only helper for verifying credential retirement on assistant deletion.
SELECT deleted
FROM assistant_mcp_oauth_clients
WHERE project_id = @project_id
  AND assistant_id = @assistant_id
  AND oauth_server_issuer = @oauth_server_issuer;

-- name: ListChatAttachmentAssets :many
-- Resolves the chat attachments a dashboard turn carries, scoped to the
-- project so a leaked asset id from another project cannot be attached.
SELECT id, name, url, content_type, content_length
FROM assets
WHERE project_id = @project_id::uuid
  AND id = ANY(@ids::uuid[])
  AND kind = 'chat_attachment'
  AND deleted IS FALSE;

-- name: SetAssistantDesiredRuntimeVersionForTest :exec
-- Test fixture only: runtime selection is not a supported mutation.
UPDATE assistants SET runtime_version = @runtime_version
WHERE id = @assistant_id AND project_id = @project_id;

-- name: CountAssistantRuntimeRowsForTest :one
SELECT count(*) FROM assistant_runtimes
WHERE assistant_id = @assistant_id AND project_id = @project_id;

-- name: AssistantHasUnsettledEvents :one
-- Unknown statuses fail closed; pending includes queued admissions.
SELECT EXISTS (
  SELECT 1 FROM assistant_thread_events e
  WHERE e.project_id = @project_id AND e.assistant_id = @assistant_id
    AND e.deleted IS FALSE AND e.status NOT IN ('completed', 'failed', 'cancelled')
) OR EXISTS (
  SELECT 1 FROM trigger_instances t
  WHERE t.project_id = @project_id AND t.target_kind = 'assistant'
    AND t.target_ref = sqlc.arg('assistant_id')::uuid::text AND t.definition_slug = 'wake'
    AND t.deleted IS FALSE AND t.status NOT IN ('fired', 'cancelled')
) AS busy;

-- name: ListAssistantRuntimesForSwitch :many
SELECT * FROM assistant_runtimes
WHERE project_id = @project_id AND assistant_id = @assistant_id
  AND deleted IS FALSE AND ended IS FALSE
ORDER BY id
FOR UPDATE;

-- name: SetAssistantDesiredRuntimeVersion :execrows
-- Caller holds the assistant advisory lock and has fenced and retired old runtimes.
UPDATE assistants SET runtime_version = @runtime_version, updated_at = clock_timestamp()
WHERE id = @assistant_id AND project_id = @project_id AND deleted IS FALSE
  AND runtime_version = @expected_runtime_version;

-- name: RetireAssistantRuntimesForSwitch :exec
-- Never rewrite a runtime's protocol. Authority checks must reject ended rows.
UPDATE assistant_runtimes
SET state = 'switch_retiring', ended_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE project_id = @project_id AND assistant_id = @assistant_id
  AND deleted IS FALSE AND ended IS FALSE;

-- name: HasPendingRuntimeSwitchRetirement :one
SELECT EXISTS (SELECT 1 FROM assistant_runtimes
 WHERE project_id = @project_id AND assistant_id = @assistant_id
 AND state = 'switch_retiring' AND ended IS TRUE);

-- name: ListPendingRuntimeSwitchRetirements :many
SELECT * FROM assistant_runtimes
WHERE state = 'switch_retiring' AND ended IS TRUE
ORDER BY updated_at ASC
LIMIT @limit_count;

-- name: CompleteRuntimeSwitchRetirement :exec
UPDATE assistant_runtimes SET state = 'stopped', updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id
 AND state = 'switch_retiring' AND ended IS TRUE;

-- name: GetLiveKitRuntimeAuthority :one
SELECT r.id
FROM assistant_runtimes r
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
WHERE r.id = @runtime_id AND r.project_id = @project_id AND r.assistant_id = @assistant_id
  AND r.state IN ('starting', 'active')
  AND r.runtime_version = 3 AND r.deleted IS FALSE AND r.ended IS FALSE
  AND a.runtime_version = 3 AND a.deleted IS FALSE
FOR SHARE OF r, a;

-- name: GetKitRuntimeControlAuthority :one
SELECT r.id
FROM assistant_runtimes r
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
WHERE r.id = @runtime_id AND r.project_id = @project_id AND r.assistant_id = @assistant_id
  AND r.state IN ('starting', 'active', 'stopped')
  AND r.runtime_version = 3 AND r.deleted IS FALSE AND r.ended IS FALSE
  AND a.runtime_version = 3 AND a.deleted IS FALSE
FOR SHARE OF r, a;

-- name: GetKitRuntimeSessionBinding :one
-- Existence only: never load any previous transcript contents into the guest.
SELECT r.id AS runtime_id, t.id AS thread_id, t.chat_id, s.acp_session_id,
  EXISTS(SELECT 1 FROM chat_messages m WHERE m.chat_id = t.chat_id) AS has_prior_transcript
FROM assistant_runtimes r
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
JOIN assistant_threads t ON t.assistant_id = a.id AND t.project_id = a.project_id
JOIN chats c ON c.id = t.chat_id AND c.project_id = t.project_id
LEFT JOIN assistant_kit_sessions s ON s.runtime_id = r.id AND s.assistant_thread_id = t.id
  AND s.project_id = r.project_id AND s.assistant_id = r.assistant_id
WHERE r.id = @runtime_id AND r.project_id = @project_id AND r.assistant_id = @assistant_id
  AND t.id = @thread_id
  AND r.runtime_version = 3 AND r.deleted IS FALSE AND r.ended IS FALSE
  AND a.runtime_version = 3 AND a.deleted IS FALSE
  AND t.deleted IS FALSE AND c.deleted IS FALSE
FOR SHARE OF r, a, t, c;

-- name: InsertKitRuntimeSession :execrows
INSERT INTO assistant_kit_sessions (project_id, assistant_id, runtime_id, assistant_thread_id, acp_session_id)
SELECT r.project_id, r.assistant_id, r.id, t.id, @session_id
FROM assistant_runtimes r
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
JOIN assistant_threads t ON t.assistant_id = a.id AND t.project_id = a.project_id
JOIN chats c ON c.id = t.chat_id AND c.project_id = t.project_id
WHERE r.id = @runtime_id AND r.project_id = @project_id AND r.assistant_id = @assistant_id
  AND t.id = @thread_id
  AND r.runtime_version = 3 AND r.deleted IS FALSE AND r.ended IS FALSE
  AND a.runtime_version = 3 AND a.deleted IS FALSE
  AND t.deleted IS FALSE AND c.deleted IS FALSE
ON CONFLICT (runtime_id, assistant_thread_id) DO NOTHING;

-- name: StoreKitGatewayCredential :execrows
INSERT INTO assistant_kit_gateway_credentials (project_id, assistant_id, runtime_id, token_ciphertext)
SELECT r.project_id, r.assistant_id, r.id, @token_ciphertext
FROM assistant_runtimes r
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
WHERE r.id = @runtime_id AND r.project_id = @project_id AND r.assistant_id = @assistant_id
  AND r.runtime_version = 3 AND r.deleted IS FALSE AND r.ended IS FALSE
  AND a.runtime_version = 3 AND a.deleted IS FALSE
ON CONFLICT (runtime_id) DO UPDATE
SET token_ciphertext = EXCLUDED.token_ciphertext, updated_at = clock_timestamp()
WHERE assistant_kit_gateway_credentials.project_id = EXCLUDED.project_id
  AND assistant_kit_gateway_credentials.assistant_id = EXCLUDED.assistant_id;

-- name: LoadKitGatewayCredential :one
SELECT g.token_ciphertext
FROM assistant_kit_gateway_credentials g
JOIN assistant_runtimes r ON r.id = g.runtime_id AND r.project_id = g.project_id AND r.assistant_id = g.assistant_id
JOIN assistants a ON a.id = r.assistant_id AND a.project_id = r.project_id
WHERE r.id = @runtime_id AND r.project_id = @project_id AND r.assistant_id = @assistant_id
  AND r.runtime_version = 3 AND r.deleted IS FALSE AND r.ended IS FALSE
  AND a.runtime_version = 3 AND a.deleted IS FALSE;

-- name: ListKitProtocolSessions :many
SELECT s.acp_session_id, c.id AS chat_id
FROM assistant_kit_sessions s
LEFT JOIN assistant_threads t ON t.id = s.assistant_thread_id
  AND t.project_id = s.project_id AND t.assistant_id = s.assistant_id AND t.deleted IS FALSE
LEFT JOIN chats c ON c.id = t.chat_id AND c.project_id = s.project_id AND c.deleted IS FALSE
WHERE s.project_id = @project_id AND s.assistant_id = @assistant_id AND s.runtime_id = @runtime_id
ORDER BY s.acp_session_id;

-- name: KitProtocolHasPriorMessages :one
SELECT EXISTS(SELECT 1 FROM chat_messages
WHERE project_id= @project_id AND chat_id= @chat_id
AND id <> ALL(@current_message_ids::uuid[])) AS has_prior;

-- name: ResolveKitCurrentUserMessage :many
-- Dashboard idempotency_key is the caller's message ID, not an event UUID or text match.
SELECT m.id FROM assistant_thread_events e
JOIN assistant_threads t ON t.id=e.assistant_thread_id AND t.project_id=e.project_id
JOIN chat_messages m ON m.chat_id=t.chat_id AND m.project_id=e.project_id AND m.role='user'
WHERE e.project_id= @project_id AND e.assistant_id= @assistant_id AND e.id= @event_id
AND e.deleted IS FALSE AND t.deleted IS FALSE
AND t.source_kind='dashboard'
AND NULLIF(e.source_payload_json->>'idempotency_key','') IS NOT NULL
AND (m.message_id=e.source_payload_json->>'idempotency_key' OR m.id::text=e.source_payload_json->>'idempotency_key');

-- name: StoreKitProtocolUserEcho :execrows
UPDATE assistant_thread_events e
SET source_payload_json=jsonb_set(e.source_payload_json,'{_kit_user_echoes}',
COALESCE(e.source_payload_json->'_kit_user_echoes','[]'::jsonb) || @echo::jsonb), updated_at=clock_timestamp()
WHERE e.project_id= @project_id AND e.assistant_id= @assistant_id AND e.assistant_thread_id= @thread_id
AND e.id= @event_id AND e.deleted IS FALSE
AND EXISTS(SELECT 1 FROM chat_messages m JOIN assistant_threads t ON t.chat_id=m.chat_id
WHERE t.id=e.assistant_thread_id AND t.project_id=e.project_id
AND m.project_id=e.project_id AND m.id= @message_id AND m.role='user');

-- name: ListKitProtocolUserEchoes :many
SELECT (e.source_payload_json->'_kit_user_echoes')::jsonb AS echoes
FROM assistant_thread_events e
WHERE e.project_id= @project_id AND e.assistant_id= @assistant_id AND e.assistant_thread_id= @thread_id
AND e.deleted IS FALSE AND e.source_payload_json ? '_kit_user_echoes';

-- name: SetKitRuntimeMetadataFixture :exec
-- Test fixture for replacement authority and legacy metadata inheritance.
UPDATE assistant_runtimes
SET runtime_version = @runtime_version, backend_metadata_json = @metadata::jsonb
WHERE project_id = @project_id AND id = @runtime_id;

-- name: SetKitThreadSourceFixture :exec
-- Test fixture for authoritative dashboard input correlation.
UPDATE assistant_threads SET source_kind = @source_kind
WHERE project_id = @project_id AND id = @thread_id;

-- name: RequestKitInterrupt :one
-- Caller holds the assistant admission fence; never changes event status.
INSERT INTO assistant_kit_interrupt_requests
  (project_id, assistant_id, runtime_id, assistant_thread_id, event_id, attempt)
SELECT e.project_id, e.assistant_id, r.id, e.assistant_thread_id, e.id, e.attempts
FROM assistant_thread_events e
JOIN assistant_runtimes r ON r.project_id=e.project_id AND r.assistant_id=e.assistant_id
WHERE e.project_id=sqlc.arg(project_id)::uuid AND e.assistant_id=sqlc.arg(assistant_id)::uuid
  AND e.assistant_thread_id=sqlc.arg(thread_id)::uuid AND r.id=sqlc.arg(runtime_id)::uuid
  AND e.deleted IS FALSE AND e.status='processing' AND e.attempts > 0
  AND r.deleted IS FALSE AND r.ended IS FALSE AND r.runtime_version=3
ORDER BY e.created_at LIMIT 1
ON CONFLICT (project_id, runtime_id, event_id, attempt)
DO UPDATE SET id=assistant_kit_interrupt_requests.id
RETURNING id;

-- name: KitInterruptPending :one
SELECT EXISTS (
 SELECT 1 FROM assistant_kit_interrupt_requests q
 JOIN assistant_thread_events e ON e.id=q.event_id AND e.project_id=q.project_id
   AND e.assistant_id=q.assistant_id AND e.assistant_thread_id=q.assistant_thread_id
 JOIN assistant_runtimes r ON r.id=q.runtime_id AND r.project_id=q.project_id AND r.assistant_id=q.assistant_id
 WHERE q.project_id=sqlc.arg(project_id)::uuid AND q.runtime_id=sqlc.arg(runtime_id)::uuid
   AND q.event_id=sqlc.arg(event_id)::uuid AND q.attempt=sqlc.arg(attempt)::bigint
   AND q.outcome='pending' AND e.deleted IS FALSE AND e.status='processing' AND e.attempts=q.attempt
   AND r.deleted IS FALSE AND r.ended IS FALSE AND r.runtime_version=3
)::boolean AS pending;

-- name: AcknowledgeKitInterrupt :execrows
-- Only the owning turn calls this after authoritative settlement AND projector flush.
UPDATE assistant_kit_interrupt_requests q SET outcome=sqlc.arg(outcome)::text, acknowledged_at=clock_timestamp()
WHERE q.project_id=sqlc.arg(project_id)::uuid AND q.runtime_id=sqlc.arg(runtime_id)::uuid
  AND q.event_id=sqlc.arg(event_id)::uuid AND q.attempt=sqlc.arg(attempt)::bigint AND q.outcome='pending'
  AND EXISTS (SELECT 1 FROM assistant_thread_events e
    JOIN assistant_runtimes r ON r.id=q.runtime_id AND r.project_id=e.project_id AND r.assistant_id=e.assistant_id
    WHERE e.id=q.event_id AND e.project_id=q.project_id AND e.assistant_id=q.assistant_id
      AND e.assistant_thread_id=q.assistant_thread_id AND e.attempts=q.attempt
      AND e.deleted IS FALSE AND e.status='processing' AND r.deleted IS FALSE AND r.ended IS FALSE
    FOR UPDATE OF e);

-- name: GetKitInterruptOutcome :one
SELECT (CASE
 WHEN q.outcome <> 'pending' THEN q.outcome
 WHEN e.id IS NULL OR e.deleted OR e.attempts <> q.attempt THEN 'unknown'
 WHEN e.status <> 'processing' THEN 'unknown'
 ELSE 'pending' END)::text AS outcome
FROM assistant_kit_interrupt_requests q
LEFT JOIN assistant_thread_events e ON e.id=q.event_id AND e.project_id=q.project_id
  AND e.assistant_id=q.assistant_id AND e.assistant_thread_id=q.assistant_thread_id
WHERE q.project_id=sqlc.arg(project_id)::uuid AND q.id=sqlc.arg(request_id)::uuid;

-- name: HasPendingKitInterruptRequest :one
-- Unlike KitInterruptPending, retain pending requests after ownership is lost.
SELECT EXISTS (SELECT 1 FROM assistant_kit_interrupt_requests
 WHERE project_id = sqlc.arg(project_id)::uuid AND runtime_id = sqlc.arg(runtime_id)::uuid
 AND event_id = sqlc.arg(event_id)::uuid AND attempt = @attempt AND outcome = 'pending');

-- name: SetAssistantRuntimeBackendForTest :exec
UPDATE assistant_runtimes SET backend = @backend
WHERE id = @runtime_id AND project_id = @project_id;

-- name: EndAssistantRuntimeForTest :exec
UPDATE assistant_runtimes SET ended_at = clock_timestamp()
WHERE id = @runtime_id AND project_id = @project_id;

-- name: SetAttachedMCPServerIssuerForTest :exec
UPDATE mcp_servers SET user_session_issuer_id = @user_session_issuer_id
WHERE id = @id AND project_id = @project_id;

-- name: DisableAttachedMCPServerForTest :exec
UPDATE mcp_servers SET visibility = 'disabled'
WHERE id = @id AND project_id = @project_id;

-- name: AcquireAssistantSessionLock :exec
-- Pin the connection until ReleaseAssistantSessionLock completes.
SELECT pg_advisory_lock(hashtext('asst:' || @assistant_id::text));

-- name: ReleaseAssistantSessionLock :one
SELECT pg_advisory_unlock(hashtext('asst:' || @assistant_id::text))::boolean AS unlocked;

-- name: GetAssistantTransactionIsolationForTest :one
SELECT current_setting('transaction_isolation')::text AS isolation;

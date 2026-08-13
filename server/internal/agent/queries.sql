-- name: GetAgentPluginSet :many
-- Returns the device agent's full plugin set for an org, marketplace-first.
--
-- The base is the org's *published* marketplaces (plugin_github_connections rows
-- with a marketplace_token), scoped so the device agent isn't flooded with every
-- project: the org's default project always appears (marketplace + its
-- always-required observability plugin, synthesized in the view layer) as the
-- org-wide baseline, while a non-default project appears only when the caller has
-- a matching assignment there. Plugins whose assignment principal_urn matches the
-- caller's resolved principal set (email, user:<id>, user:all, role:<...>, or the
-- org wildcard) are LEFT JOINed on top; the default project still yields one row
-- with null plugin columns when the caller has no assignment there.
--
-- Each project resolves to a marketplace name the way the publish path does:
-- the per-project override (project_marketplace_settings.marketplace_name) when
-- set, else the org-derived default (computed in the view). Projects with
-- distinct names surface as distinct marketplaces; projects that share a name
-- (e.g. several on the org default) still collapse to one in the view.
--
-- Rows are ordered by pr.id so the org's default project (first by id ASC, the
-- one created at org setup) sorts first. When projects share a name and the view
-- collapses them, keeping the first row's token makes that collapse resolve to
-- the default project rather than the arbitrary alphabetically-first one.
SELECT
  pr.id AS project_id,
  pr.slug AS project_slug,
  om.slug AS organization_slug,
  om.name AS organization_name,
  pgc.marketplace_token,
  pgc.updated_at AS marketplace_updated_at,
  -- The hooks subtree may be pinned by the rollout gate under a pre-rename org
  -- name; the view derives the observability slug from this snapshot so devices
  -- install the plugin that actually exists in the published repo.
  pgc.published_hooks_config,
  pms.marketplace_name AS marketplace_name_override,
  -- The org's default project (oldest, by id ASC over ALL non-deleted projects,
  -- not just published ones) keeps the bare org-derived marketplace name; others
  -- are project-scoped. Resolved the same way the publish path does so the names
  -- match. The subquery spans unpublished projects too, so an unpublished default
  -- doesn't hand the bare name to a different project.
  (pr.id = (
    -- Pinned to the @organization_id parameter (not pr.organization_id) so this
    -- stays uncorrelated and Postgres evaluates the org's default project once,
    -- not per row.
    SELECT p2.id
    FROM projects p2
    WHERE p2.organization_id = @organization_id
      AND p2.deleted IS FALSE
    ORDER BY p2.id ASC
    LIMIT 1
  )) AS is_default_project,
  p.id AS plugin_id,
  p.slug AS plugin_slug,
  p.updated_at AS plugin_updated_at
FROM plugin_github_connections pgc
JOIN projects pr
  ON pr.id = pgc.project_id
  AND pr.deleted IS FALSE
JOIN organization_metadata om
  ON om.id = pr.organization_id
LEFT JOIN project_marketplace_settings pms
  ON pms.project_id = pr.id
LEFT JOIN plugins p
  ON p.project_id = pr.id
  AND p.deleted IS FALSE
  AND EXISTS (
    SELECT 1 FROM plugin_assignments pa
    WHERE pa.plugin_id = p.id
      AND pa.organization_id = @organization_id
      AND pa.principal_urn = ANY(@principal_urns::text[])
  )
WHERE pr.organization_id = @organization_id
  AND pgc.marketplace_token IS NOT NULL
  AND (
    -- The org's default project (oldest, by id ASC) is the org-wide baseline:
    -- always surface its marketplace + observability, even when the caller has no
    -- assignment there. Pinned to @organization_id (uncorrelated) so Postgres
    -- evaluates it once; the same subquery backs the is_default_project column.
    pr.id = (
      SELECT p2.id
      FROM projects p2
      WHERE p2.organization_id = @organization_id
        AND p2.deleted IS FALSE
      ORDER BY p2.id ASC
      LIMIT 1
    )
    -- A non-default project surfaces only when the caller has a matching assigned
    -- plugin there (the LEFT JOIN produced a non-null plugin row); otherwise its
    -- marketplace and observability plugin are just noise for this user.
    OR p.id IS NOT NULL
  )
ORDER BY pr.id, p.slug;

-- name: UpsertDeviceAgentSync :exec
-- Best-effort record that the device agent for @email in @organization_id polled.
-- The ON CONFLICT WHERE guard caps writes to at most once per minute per
-- (org, email) so the agent's ~60s poll cadence doesn't create write spikes,
-- mirroring how api_keys.last_accessed_at is throttled.
INSERT INTO device_agent_syncs (organization_id, email)
VALUES (@organization_id, @email)
ON CONFLICT (organization_id, email) DO UPDATE
SET last_seen_at = clock_timestamp()
  , updated_at   = clock_timestamp()
WHERE device_agent_syncs.last_seen_at < clock_timestamp() - interval '1 minute';

-- name: UpsertDeviceAgentDeviceSync :exec
-- Best-effort record that the agent on the machine bearing @serial_number
-- polled. Sibling of UpsertDeviceAgentSync: that one answers "does this user
-- run the agent somewhere", this one answers "does THIS machine run it".
-- Only called when the agent reported a serial.
--
-- The guard extends the sibling's once-a-minute heartbeat throttle with two
-- change conditions. Throttling alone would be wrong here: the row carries
-- mutable descriptive columns, and at a ~60s poll cadence last_seen_at is
-- almost always fresh, so a reassigned machine's new user (or a rename)
-- could go unrecorded for the entire session.
INSERT INTO device_agent_device_syncs (organization_id, serial_number, email, hostname)
VALUES (@organization_id, @serial_number, @email, sqlc.narg('hostname'))
-- Infers device_agent_device_syncs_org_lower_serial_key, the unique
-- expression index that is this table's dedup key. Matching the readers'
-- LOWER() comparison is what stops one machine from holding two rows.
ON CONFLICT (organization_id, LOWER(serial_number)) DO UPDATE
SET last_seen_at = clock_timestamp()
  , updated_at   = clock_timestamp()
  , email        = EXCLUDED.email
    -- An agent that stopped reporting a hostname must not blank a known one.
  , hostname     = COALESCE(EXCLUDED.hostname, device_agent_device_syncs.hostname)
WHERE device_agent_device_syncs.last_seen_at < clock_timestamp() - interval '1 minute'
   OR device_agent_device_syncs.email IS DISTINCT FROM EXCLUDED.email
   OR (EXCLUDED.hostname IS NOT NULL AND device_agent_device_syncs.hostname IS DISTINCT FROM EXCLUDED.hostname);

-- name: ListDeviceAgentSyncs :many
-- Lists every distinct email seen polling the device agent for an org, most
-- recently active first, for the dashboard's device-agent users view.
SELECT organization_id, email, first_seen_at, last_seen_at
FROM device_agent_syncs
WHERE organization_id = @organization_id
ORDER BY last_seen_at DESC;

-- name: GetDeviceAgentConfiguration :one
SELECT organization_id, schema_version, config, created_at, updated_at
FROM device_agent_configurations
WHERE organization_id = @organization_id;

-- AcquireDeviceAgentConfigurationLock serializes configuration updates for an
-- organization even when no row exists yet — FOR UPDATE cannot lock an absent
-- row, so two concurrent first-time saves would otherwise both audit a nil
-- before-snapshot. Transaction-scoped: released automatically at
-- commit/rollback.

-- name: AcquireDeviceAgentConfigurationLock :exec
SELECT pg_advisory_xact_lock(hashtextextended('device_agent_configurations:' || @organization_id::text, 0));

-- GetDeviceAgentConfigurationForUpdate locks the row for the update
-- transaction so the unknown-key merge and audit before-snapshot cannot read
-- a config replaced beneath them.

-- name: GetDeviceAgentConfigurationForUpdate :one
SELECT organization_id, schema_version, config, created_at, updated_at
FROM device_agent_configurations
WHERE organization_id = @organization_id
FOR UPDATE;

-- UpsertDeviceAgentConfiguration deliberately replaces the whole document:
-- @config is the already-merged result computed by UpdateConfiguration under
-- the org advisory lock, where stored keys outside the caller's replaceable
-- set (unknown keys, platform-admin-only keys for org admins) were carried
-- over. Do not call this with a raw client payload.

-- name: UpsertDeviceAgentConfiguration :one
INSERT INTO device_agent_configurations (
  organization_id,
  schema_version,
  config
)
VALUES (
  @organization_id,
  @schema_version,
  @config::jsonb
)
ON CONFLICT (organization_id) DO UPDATE
SET schema_version = EXCLUDED.schema_version
  , config = EXCLUDED.config
  , updated_at = clock_timestamp()
RETURNING organization_id, schema_version, config, created_at, updated_at;

-- name: ListOwnedChatSessionMeta :many
-- Session-picker metadata for captured agent sessions, strictly owner-matched:
-- only chats whose user_id is the authenticated per-user-key owner, in the
-- key's enrolled project. Personal-account sessions are included (decision on
-- session-portability question Q2, 2026-08-10): the caller is the
-- authenticated owner reading their own metadata. Revisit before any endpoint
-- serves session CONTENT or admin-facing listings — personal-account
-- ownership attribution is partly device-bridge-inferred, which is acceptable
-- for titles but not for transcripts.
SELECT c.id, c.title, c.updated_at
FROM chats c
WHERE c.id = ANY(@chat_ids::uuid[])
  AND c.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.user_id = @user_id::text
  AND c.deleted IS FALSE;

-- name: GetChatTitleForMove :one
-- Best-effort display enrichment for the chat_session:move audit entry. The
-- move is recorded even when this returns no rows (the session may not have
-- been captured yet), so callers treat ErrNoRows as empty, not failure.
SELECT c.title, c.user_id
FROM chats c
WHERE c.id = @id
  AND c.project_id = @project_id
  AND c.organization_id = @organization_id
  AND c.deleted IS FALSE;

-- name: InsertSessionHandoffLink :one
-- Mint a session-handoff capability link. The token is the capability; TTL
-- and burn-after-read (consumed_at) bound a leaked link's exposure window.
-- Minting goes through the tenant-qualified project so a caller whose project
-- and organization disagree, or whose project is soft-deleted, gets no row
-- rather than a live capability URL.
INSERT INTO session_handoff_links (
  project_id, organization_id, session_id, token, content, created_by_email, expires_at
)
SELECT p.id, p.organization_id, @session_id, @token, @content, @created_by_email, @expires_at
FROM projects p
WHERE p.id = @project_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
RETURNING id, expires_at;

-- name: ConsumeSessionHandoffLink :one
-- Atomically claim a link on read: exactly one caller can flip consumed_at,
-- so a raced second fetch loses and gets no rows — burn-after-read without a
-- separate lock. Expired, already-consumed, and links whose project has since
-- been soft-deleted all return no rows; callers must serve every case as an
-- indistinguishable 404.
--
-- The claim reads the document out of a locked subquery and blanks the stored
-- copy in the same statement, so burning a link also destroys the server's
-- copy of the transcript instead of leaving it in Postgres forever. RETURNING
-- reads from the subquery because the outer UPDATE's own RETURNING would hand
-- back the blanked value.
UPDATE session_handoff_links s
SET consumed_at = clock_timestamp(), updated_at = clock_timestamp(), content = ''
FROM (
  SELECT l.id, l.content
  FROM session_handoff_links l
  JOIN projects p ON p.id = l.project_id
  WHERE l.token = @token
    AND l.consumed_at IS NULL
    AND l.expires_at > clock_timestamp()
    AND p.deleted IS FALSE
  FOR UPDATE OF l
) claimed
WHERE s.id = claimed.id
RETURNING claimed.content;

-- name: GetServedSkillRecipient :one
-- The authenticated caller as a recipient of served skills: their email, read
-- only while they are a live user holding an active membership in the enabled
-- organization that owns the endpoint. Delivery principals are resolved from
-- the row, so a user removed from the organization, or deleted, whose session
-- or token still validates is no row and so no recipient. The membership
-- predicate is the one IsOrganizationMember applies at login.
SELECT u.email
FROM users u
WHERE u.id = @user_id
  AND u.deleted_at IS NULL
  AND u.workos_deleted_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM organization_user_relationships our
    JOIN organization_metadata om
      ON om.id = our.organization_id
      AND om.disabled_at IS NULL
    WHERE our.organization_id = @organization_id
      AND our.user_id = u.id
      AND our.deleted_at IS NULL
  );

-- name: ListServedSkillsForEndpoint :many
-- The skills a hosted toolset endpoint serves over MCP to one caller, one row
-- per skill name, ordered by name. A candidate is an active plugin-channel
-- distribution whose plugin belongs to the endpoint's project, is not deleted,
-- contains the endpoint's backend (the toolset's canonical mcp_servers row,
-- which shares the toolset's id, or the mcp_servers row fronting it when the
-- caller passes one), and is assigned to at least one of
-- the caller's principal URNs. The skill must not be archived. Each candidate
-- carries the distribution's resolved version: the pin when one is set,
-- otherwise the latest valid version under the same ordering as
-- GetLatestValidSkillVersion.
--
-- A skill carried by several matching plugins has several candidates, and the
-- query keeps one: a pinned candidate beats one that tracks the latest
-- version, and between pins the newest resolved version (by effective time,
-- then version id) wins; the plugin id breaks a tie between two
-- distributions of the same version. pinned_versions counts the distinct
-- versions the skill is pinned to across its candidates, so a caller can tell
-- when pins disagree. The limit counts skills, not candidates, so a skill
-- carried by many plugins never crowds out a later skill by name. An empty
-- principal list matches nothing.
WITH candidates AS (
  SELECT
      sk.id AS skill_id,
      sk.name AS skill_name,
      p.id AS plugin_id,
      (sd.pinned_version_id IS NOT NULL)::boolean AS pinned,
      resolved.id AS version_id,
      resolved.content,
      resolved.raw_sha256,
      resolved.effective_at
  FROM skill_distributions sd
  JOIN skills sk
    ON sk.id = sd.skill_id
    AND sk.project_id = sd.project_id
    AND sk.archived_at IS NULL
  JOIN plugins p
    ON p.id = sd.plugin_id
    AND p.project_id = sd.project_id
    AND p.deleted IS FALSE
  JOIN LATERAL (
    SELECT
      sv.id,
      sv.content,
      sv.raw_sha256,
      COALESCE(sv.promoted_at, sv.created_at)::timestamptz AS effective_at
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
  WHERE sd.project_id = @project_id
    AND sd.channel = 'plugin'
    AND sd.assistant_id IS NULL
    AND sd.revoked_at IS NULL
    AND (sqlc.narg(skill_name)::text IS NULL OR sk.name = sqlc.narg(skill_name)::text)
    AND EXISTS (
      SELECT 1
      FROM plugin_servers ps
      WHERE ps.plugin_id = p.id
        AND ps.deleted IS FALSE
        AND (
          ps.mcp_server_id = @toolset_id::uuid
          OR (
            sqlc.narg(mcp_server_id)::uuid IS NOT NULL
            AND ps.mcp_server_id = sqlc.narg(mcp_server_id)::uuid
          )
        )
    )
    AND EXISTS (
      SELECT 1
      FROM plugin_assignments pa
      WHERE pa.plugin_id = p.id
        AND pa.organization_id = p.organization_id
        AND pa.principal_urn = ANY(@principal_urns::text[])
    )
),
pins AS (
  SELECT skill_name, COUNT(DISTINCT version_id)::int AS pinned_versions
  FROM candidates
  WHERE pinned
  GROUP BY skill_name
)
SELECT DISTINCT ON (c.skill_name)
    c.skill_id,
    c.skill_name,
    c.plugin_id,
    c.pinned,
    c.version_id,
    c.content,
    c.raw_sha256,
    c.effective_at,
    COALESCE(pins.pinned_versions, 0)::int AS pinned_versions
FROM candidates c
LEFT JOIN pins ON pins.skill_name = c.skill_name
ORDER BY c.skill_name ASC, c.pinned DESC, c.effective_at DESC, c.version_id DESC, c.plugin_id ASC
LIMIT @row_limit;

-- name: InsertMCPSkillObservation :execrows
-- Records one resources/read of a served skill as an activation. The server
-- already knows which version it served, so the row arrives reconciled
-- (skill_id, skill_version_id and reconciled_at set) and the sighting counters
-- on the skill move in the same statement, which is the part of
-- CompleteSkillObservations the hook reconciler would otherwise apply. The row
-- is also born complete for the two pipelines that drain reconciled,
-- session-bearing observations: metrics_synced_at and efficacy_enqueued_at are
-- stamped at insert, because an MCP session id is neither a dev nor an
-- assistant session for the ClickHouse session sync and names no chat
-- transcript to score. Left NULL, the row would keep the project pending for
-- both sweeps while their batch queries skip it, and the work would never
-- drain.
-- Returns 1 when the read was recorded and 0 when the idempotency key was
-- already seen in the project, so a re-read in one session counts once. A NULL
-- idempotency key records every read.
WITH inserted AS (
  INSERT INTO skill_observations (
      project_id
    , idempotency_key
    , provider
    , user_id
    , session_id
    , skill_name
    , source
    , source_level
    , source_path
    , raw_sha256
    , seen_at
    , skill_id
    , skill_version_id
    , reconciled_at
    , metrics_synced_at
    , efficacy_enqueued_at
  )
  SELECT
      s.project_id
    , sqlc.narg(idempotency_key)::text
    , @provider::text
    , sqlc.narg(user_id)::text
    , sqlc.narg(session_id)::text
    , s.name
    , @source::text
    , @source_level::text
    , @source_path::text
    , sv.raw_sha256
    , @seen_at::timestamptz
    , s.id
    , sv.id
    , clock_timestamp()
    , clock_timestamp()
    , clock_timestamp()
  FROM skills s
  JOIN skill_versions sv
    ON sv.skill_id = s.id
  WHERE s.project_id = @project_id
    AND s.id = @skill_id
    AND sv.id = @skill_version_id
  ON CONFLICT (project_id, idempotency_key) WHERE idempotency_key IS NOT NULL
  DO NOTHING
  RETURNING skill_id, seen_at
)
UPDATE skills s
SET first_seen_at = LEAST(COALESCE(s.first_seen_at, inserted.seen_at), inserted.seen_at),
    last_seen_at = GREATEST(COALESCE(s.last_seen_at, inserted.seen_at), inserted.seen_at),
    seen_count = s.seen_count + 1
FROM inserted
WHERE s.project_id = @project_id
  AND s.id = inserted.skill_id;

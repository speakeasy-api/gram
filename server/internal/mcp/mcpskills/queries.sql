-- name: IsServedSkillRecipient :one
-- Whether the authenticated caller may receive served skills: a live user
-- holding an active membership in the enabled organization that owns the
-- endpoint. A user removed from the organization, or deleted, whose session or
-- token still validates is not a recipient. The membership predicate is the
-- one IsOrganizationMember applies at login.
SELECT EXISTS (
  SELECT 1
  FROM users u
  JOIN organization_user_relationships our
    ON our.user_id = u.id
    AND our.organization_id = @organization_id
    AND our.deleted_at IS NULL
  JOIN organization_metadata om
    ON om.id = our.organization_id
    AND om.disabled_at IS NULL
  WHERE u.id = @user_id
    AND u.deleted_at IS NULL
    AND u.workos_deleted_at IS NULL
) AS recipient;

-- name: ListServedSkillsForTarget :many
-- The skills an MCP surface serves over the Skills extension, one row per
-- skill name, ordered by name. A candidate is a live mcp_skill_links row in
-- the surface's organization that names the surface: an mcp_server link to
-- any of @mcp_server_ids (the surface's own server, plus the member servers a
-- gateway inherits), a meta_mcp_server link to @meta_mcp_server_id, or a link
-- of the row-less kind @row_less_target_kind (such as the organization
-- gateway). The skill must not be archived. Each candidate carries the link's
-- resolved version: the pin when one is set, otherwise the latest valid
-- version under the same ordering as GetLatestValidSkillVersion.
--
-- The organization, not one project, bounds the read because a gateway spans
-- every project in it; each link's project is pinned to its skill's by
-- foreign key.
--
-- A skill linked several times has several candidates, and the query keeps
-- one: a pinned candidate beats one that tracks the latest version, and
-- between pins the newest resolved version (by effective time, then version
-- id) wins; the link id breaks a tie between two links of the same version.
-- pinned_versions counts the distinct versions the skill is pinned to across
-- its candidates, so a caller can tell when pins disagree. The limit counts
-- skills, not candidates, so a skill linked many times never crowds out a
-- later skill by name.
WITH candidates AS (
  SELECT
      sk.id AS skill_id,
      sk.project_id,
      sk.name AS skill_name,
      l.id AS link_id,
      (l.pinned_version_id IS NOT NULL)::boolean AS pinned,
      resolved.id AS version_id,
      resolved.content,
      resolved.raw_sha256,
      resolved.effective_at
  FROM mcp_skill_links l
  JOIN projects p
    ON p.id = l.project_id
    AND p.organization_id = l.organization_id
    AND p.deleted IS FALSE
  JOIN skills sk
    ON sk.id = l.skill_id
    AND sk.project_id = l.project_id
    AND sk.archived_at IS NULL
  JOIN LATERAL (
    SELECT
      sv.id,
      sv.content,
      sv.raw_sha256,
      COALESCE(sv.promoted_at, sv.created_at)::timestamptz AS effective_at
    FROM skill_versions sv
    LEFT JOIN skill_version_origins svo
      ON svo.project_id = l.project_id
      AND svo.skill_id = sv.skill_id
      AND svo.skill_version_id = sv.id
    WHERE sv.skill_id = l.skill_id
      AND sv.spec_valid IS TRUE
      AND (l.pinned_version_id IS NULL OR sv.id = l.pinned_version_id)
    ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, COALESCE(sv.promoted_at, sv.created_at) DESC, sv.id DESC
    LIMIT 1
  ) resolved ON TRUE
  WHERE l.organization_id = @organization_id
    AND l.deleted IS FALSE
    AND (sqlc.narg(skill_name)::text IS NULL OR sk.name = sqlc.narg(skill_name)::text)
    AND (
      (l.target_kind = 'mcp_server' AND l.mcp_server_id = ANY(@mcp_server_ids::uuid[]))
      OR (
        l.target_kind = 'meta_mcp_server'
        AND sqlc.narg(meta_mcp_server_id)::uuid IS NOT NULL
        AND l.meta_mcp_server_id = sqlc.narg(meta_mcp_server_id)::uuid
      )
      OR (
        sqlc.narg(row_less_target_kind)::text IS NOT NULL
        AND l.target_kind = sqlc.narg(row_less_target_kind)::text
      )
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
    c.project_id,
    c.skill_name,
    c.link_id,
    c.pinned,
    c.version_id,
    c.content,
    c.raw_sha256,
    c.effective_at,
    COALESCE(pins.pinned_versions, 0)::int AS pinned_versions
FROM candidates c
LEFT JOIN pins ON pins.skill_name = c.skill_name
ORDER BY c.skill_name ASC, c.pinned DESC, c.effective_at DESC, c.version_id DESC, c.link_id ASC
LIMIT @row_limit;

-- name: CreateMCPSkillLink :one
-- Links a skill to the MCP surface that serves it. The caller validates that
-- the id columns fit target_kind; the foreign keys keep the skill, its pinned
-- version and any stored target inside the link's project.
INSERT INTO mcp_skill_links (
    organization_id
  , project_id
  , skill_id
  , pinned_version_id
  , target_kind
  , mcp_server_id
  , meta_mcp_server_id
  , created_by_user_id
)
VALUES (
    @organization_id
  , @project_id
  , @skill_id
  , sqlc.narg(pinned_version_id)
  , @target_kind
  , sqlc.narg(mcp_server_id)
  , sqlc.narg(meta_mcp_server_id)
  , @created_by_user_id
)
RETURNING *;

-- name: DeleteMCPSkillLink :execrows
-- Soft-deletes one live link, so the surface stops serving the skill through
-- it.
UPDATE mcp_skill_links
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE;

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

-- name: ListMCPSkillObservations :many
-- Test fixture: the activations one project recorded over MCP, oldest first.
SELECT *
FROM skill_observations
WHERE project_id = @project_id
  AND provider = @provider::text
ORDER BY seen_at ASC, id ASC;

-- name: GetLinkableMCPServer :one
-- A live MCP server in the caller's project that a skill can be linked to,
-- with the name a link view shows and the toolset its grants are keyed on.
SELECT
    ms.id,
    ms.project_id,
    ms.toolset_id,
    COALESCE(ms.name, t.name, ms.slug, ms.id::text)::text AS name
FROM mcp_servers ms
JOIN projects p
  ON p.id = ms.project_id
  AND p.organization_id = @organization_id
  AND p.deleted IS FALSE
LEFT JOIN toolsets t
  ON t.id = ms.toolset_id
  AND t.project_id = ms.project_id
  AND t.deleted IS FALSE
WHERE ms.project_id = @project_id
  AND ms.id = @id
  AND ms.deleted IS FALSE;

-- name: GetLinkableMetaMCPServer :one
-- A live meta MCP gateway in the caller's project that a skill can be linked
-- to.
SELECT
    mm.id,
    mm.project_id,
    mm.name
FROM meta_mcp_servers mm
WHERE mm.organization_id = @organization_id
  AND mm.project_id = @project_id
  AND mm.id = @id
  AND mm.deleted IS FALSE;

-- name: GetLiveMCPSkillLinkForTarget :one
-- The live link of one skill to one target, if any. Row-less targets match on
-- their kind with both id columns NULL, as the unique index treats them.
SELECT *
FROM mcp_skill_links
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND skill_id = @skill_id
  AND target_kind = @target_kind
  AND mcp_server_id IS NOT DISTINCT FROM sqlc.narg(mcp_server_id)::uuid
  AND meta_mcp_server_id IS NOT DISTINCT FROM sqlc.narg(meta_mcp_server_id)::uuid
  AND deleted IS FALSE;

-- name: UpsertMCPSkillLink :one
-- Links a skill to a target, or updates the pin of the live link that
-- already joins them, so repeating the same request is idempotent. updated_at
-- moves only when the pin changes. The caller validates that the id columns
-- fit target_kind; the foreign keys keep the skill, its pinned version and any
-- stored target inside the link's project.
INSERT INTO mcp_skill_links (
    organization_id
  , project_id
  , skill_id
  , pinned_version_id
  , target_kind
  , mcp_server_id
  , meta_mcp_server_id
  , created_by_user_id
)
VALUES (
    @organization_id
  , @project_id
  , @skill_id
  , sqlc.narg(pinned_version_id)
  , @target_kind
  , sqlc.narg(mcp_server_id)
  , sqlc.narg(meta_mcp_server_id)
  , @created_by_user_id
)
ON CONFLICT (project_id, skill_id, target_kind, mcp_server_id, meta_mcp_server_id) WHERE deleted IS FALSE
DO UPDATE SET
    pinned_version_id = EXCLUDED.pinned_version_id,
    updated_at = CASE
      WHEN mcp_skill_links.pinned_version_id IS DISTINCT FROM EXCLUDED.pinned_version_id THEN clock_timestamp()
      ELSE mcp_skill_links.updated_at
    END
RETURNING *;

-- name: GetMCPSkillLink :one
-- One link in the caller's project, live or removed, with the names a link
-- view shows. A removed link is returned so an unlink can authorize against
-- the link's skill and target before answering a repeat as a no-op. The skill
-- and target are joined without liveness filters so a link to an archived
-- skill or a removed target can still be inspected and removed.
SELECT
    sqlc.embed(l),
    s.name AS skill_name,
    s.display_name AS skill_display_name,
    ms.toolset_id AS mcp_server_toolset_id,
    ms.name AS mcp_server_name,
    ms.slug AS mcp_server_slug,
    t.name AS toolset_name,
    mm.name AS meta_mcp_server_name
FROM mcp_skill_links l
JOIN skills s
  ON s.project_id = l.project_id
  AND s.id = l.skill_id
LEFT JOIN mcp_servers ms
  ON ms.project_id = l.project_id
  AND ms.id = l.mcp_server_id
LEFT JOIN toolsets t
  ON t.project_id = ms.project_id
  AND t.id = ms.toolset_id
LEFT JOIN meta_mcp_servers mm
  ON mm.project_id = l.project_id
  AND mm.id = l.meta_mcp_server_id
WHERE l.organization_id = @organization_id
  AND l.project_id = @project_id
  AND l.id = @id;

-- name: ListMCPSkillLinks :many
-- A page of the caller's project's live links, oldest first, with the names
-- a link view shows. Links of archived skills and links whose stored target
-- was removed are left out, since neither serves anything. Every filter is
-- optional.
SELECT
    sqlc.embed(l),
    s.name AS skill_name,
    s.display_name AS skill_display_name,
    ms.toolset_id AS mcp_server_toolset_id,
    ms.name AS mcp_server_name,
    ms.slug AS mcp_server_slug,
    t.name AS toolset_name,
    mm.name AS meta_mcp_server_name
FROM mcp_skill_links l
JOIN skills s
  ON s.project_id = l.project_id
  AND s.id = l.skill_id
  AND s.archived_at IS NULL
LEFT JOIN mcp_servers ms
  ON ms.project_id = l.project_id
  AND ms.id = l.mcp_server_id
  AND ms.deleted IS FALSE
LEFT JOIN toolsets t
  ON t.project_id = ms.project_id
  AND t.id = ms.toolset_id
  AND t.deleted IS FALSE
LEFT JOIN meta_mcp_servers mm
  ON mm.project_id = l.project_id
  AND mm.id = l.meta_mcp_server_id
  AND mm.deleted IS FALSE
WHERE l.organization_id = @organization_id
  AND l.project_id = @project_id
  AND l.deleted IS FALSE
  AND (l.mcp_server_id IS NULL OR ms.id IS NOT NULL)
  AND (l.meta_mcp_server_id IS NULL OR mm.id IS NOT NULL)
  AND (sqlc.narg(skill_id)::uuid IS NULL OR l.skill_id = sqlc.narg(skill_id)::uuid)
  AND (sqlc.narg(mcp_server_id)::uuid IS NULL OR l.mcp_server_id = sqlc.narg(mcp_server_id)::uuid)
  AND (sqlc.narg(meta_mcp_server_id)::uuid IS NULL OR l.meta_mcp_server_id = sqlc.narg(meta_mcp_server_id)::uuid)
  AND (sqlc.narg(target_kind)::text IS NULL OR l.target_kind = sqlc.narg(target_kind)::text)
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (l.created_at, l.id) > (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY l.created_at ASC, l.id ASC
LIMIT @page_limit;

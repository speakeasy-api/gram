-- name: LockSkillName :exec
SELECT pg_advisory_xact_lock(hashtextextended('skill:' || (@project_id::uuid)::text || ':' || @name::text, 0));

-- name: GetSkillByNameForUpdate :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND name = @name
  AND archived_at IS NULL
FOR UPDATE;

-- name: GetSkillForUpdate :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL
FOR UPDATE;

-- name: CreateSkill :one
INSERT INTO skills (
  project_id,
  name,
  display_name,
  summary,
  source_kind,
  classification
) VALUES (
  @project_id,
  @name,
  @display_name,
  sqlc.narg(summary)::text,
  'manual',
  'custom'
)
ON CONFLICT (project_id, name) WHERE archived_at IS NULL
DO NOTHING
RETURNING *;

-- name: CreateCapturedSkill :one
INSERT INTO skills (
  project_id,
  name,
  display_name,
  summary,
  source_kind,
  classification
) VALUES (
  @project_id,
  @name,
  @display_name,
  sqlc.narg(summary)::text,
  'captured',
  'custom'
)
ON CONFLICT (project_id, name) WHERE archived_at IS NULL
DO NOTHING
RETURNING *;

-- name: CreateSkillVersion :one
INSERT INTO skill_versions (
  skill_id,
  content,
  canonical_sha256,
  raw_sha256,
  description,
  metadata,
  spec_valid,
  validation_errors,
  created_by_user_id
)
SELECT
  s.id,
  @content,
  @canonical_sha256,
  @raw_sha256,
  sqlc.narg(description)::text,
  @metadata::jsonb,
  @spec_valid,
  @validation_errors::jsonb,
  @created_by_user_id
FROM skills s
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
ON CONFLICT (skill_id, canonical_sha256)
DO NOTHING
RETURNING *;

-- name: GetSkillVersionByHash :one
SELECT sv.*
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.canonical_sha256 = @canonical_sha256;

-- name: InsertCapturedSkillVersionOrigin :exec
INSERT INTO skill_version_origins (skill_version_id, skill_id, project_id, origin)
SELECT sv.id, sv.skill_id, s.project_id, 'captured'
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND sv.id = @skill_version_id
ON CONFLICT (skill_version_id) DO NOTHING;

-- name: DeleteSkillVersionOrigin :exec
DELETE FROM skill_version_origins
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND skill_version_id = @skill_version_id;

-- name: StoreSkillRawHashAlias :one
WITH inserted AS (
  INSERT INTO skill_raw_hashes (project_id, raw_sha256, canonical_sha256)
  SELECT s.project_id, @raw_sha256, sv.canonical_sha256
  FROM skill_versions sv
  JOIN skills s ON s.id = sv.skill_id
  WHERE s.project_id = @project_id
    AND s.id = @skill_id
    AND sv.id = @skill_version_id
    AND sv.canonical_sha256 = @canonical_sha256
  ON CONFLICT (project_id, raw_sha256) DO NOTHING
  RETURNING 1
)
SELECT TRUE AS matches
FROM inserted
UNION ALL
SELECT srh.canonical_sha256 = @canonical_sha256 AS matches
FROM skill_raw_hashes srh
WHERE srh.project_id = @project_id
  AND srh.raw_sha256 = @raw_sha256
LIMIT 1;

-- name: GetSkillVersionOrigin :one
SELECT *
FROM skill_version_origins
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND skill_version_id = @skill_version_id;

-- name: GetSkillRawHash :one
SELECT *
FROM skill_raw_hashes
WHERE project_id = @project_id
  AND raw_sha256 = @raw_sha256;

-- name: UpdateSkill :one
UPDATE skills
SET display_name = @display_name,
    summary = sqlc.narg(summary)::text,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL
RETURNING *;

-- name: GetSkill :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL;

-- name: GetSkillDetails :one
SELECT
  sqlc.embed(s),
  sqlc.embed(latest),
  state.version_count
FROM skills s
JOIN LATERAL (
  SELECT
    sv.id AS latest_version_id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) state ON TRUE
JOIN skill_versions latest
  ON latest.id = state.latest_version_id
  AND latest.skill_id = s.id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL;

-- name: GetSkillState :one
SELECT
  state.latest_version_id,
  state.version_count
FROM skills s
JOIN LATERAL (
  SELECT
    sv.id AS latest_version_id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) state ON TRUE
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL;

-- name: ListSkills :many
SELECT
  sqlc.embed(s),
  latest.id AS latest_version_id,
  latest.version_count
FROM skills s
JOIN LATERAL (
  SELECT
    sv.id,
    COUNT(*) OVER()::bigint AS version_count
  FROM skill_versions sv
  WHERE sv.skill_id = s.id
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) latest ON TRUE
WHERE s.project_id = @project_id
  AND s.archived_at IS NULL
  AND (
    sqlc.narg(cursor_name)::text IS NULL
    OR s.name > sqlc.narg(cursor_name)::text
  )
ORDER BY s.name ASC
LIMIT @page_limit;

-- name: ListSkillVersions :many
SELECT sv.*
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (sv.created_at, sv.id) < (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY sv.created_at DESC, sv.id DESC
LIMIT @page_limit;

-- name: GetSkillName :one
SELECT name
FROM skills
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL;

-- name: ArchiveSkill :one
UPDATE skills
SET archived_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND archived_at IS NULL
RETURNING *;

-- name: GetValidSkillVersion :one
SELECT sv.id
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.id = @version_id
  AND sv.spec_valid IS TRUE;

-- name: GetLatestValidSkillVersion :one
SELECT sv.id
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id
LEFT JOIN skill_version_origins svo
  ON svo.project_id = s.project_id
  AND svo.skill_id = sv.skill_id
  AND svo.skill_version_id = sv.id
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.spec_valid IS TRUE
ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
LIMIT 1;

-- name: GetPluginForDistribution :one
-- The share lock makes distribution creation serialize against plugin
-- deletion, which soft-deletes the plugin row before revoking distributions.
SELECT id, name
FROM plugins
WHERE id = @plugin_id
  AND project_id = @project_id
  AND deleted IS FALSE
FOR SHARE;

-- name: GetActiveSkillDistributionRecord :one
SELECT
  sqlc.embed(sd),
  resolved.id AS resolved_version_id
FROM skill_distributions sd
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = sd.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE sd.project_id = @project_id
  AND sd.skill_id = @skill_id
  AND sd.plugin_id = @plugin_id
  AND sd.channel = 'plugin'
  AND sd.revoked_at IS NULL
FOR UPDATE OF sd;

-- name: ListActiveSkillDistributions :many
SELECT
  sqlc.embed(sd),
  s.name AS skill_name,
  s.display_name AS skill_display_name,
  pl.name AS plugin_name,
  resolved.id AS resolved_version_id
FROM skill_distributions sd
JOIN plugins pl ON pl.id = sd.plugin_id
JOIN skills s
  ON s.project_id = sd.project_id
  AND s.id = sd.skill_id
  AND s.archived_at IS NULL
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  LEFT JOIN skill_version_origins svo
    ON svo.project_id = sd.project_id
    AND svo.skill_id = sv.skill_id
    AND svo.skill_version_id = sv.id
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE sd.project_id = @project_id
  AND sd.channel = 'plugin'
  AND sd.revoked_at IS NULL
  AND (sqlc.narg(skill_id)::uuid IS NULL OR sd.skill_id = sqlc.narg(skill_id)::uuid)
  AND (sqlc.narg(plugin_id)::uuid IS NULL OR sd.plugin_id = sqlc.narg(plugin_id)::uuid)
  AND (
    sqlc.narg(cursor_created_at)::timestamptz IS NULL
    OR (sd.created_at, sd.id) > (
      sqlc.narg(cursor_created_at)::timestamptz,
      sqlc.narg(cursor_id)::uuid
    )
  )
ORDER BY sd.created_at ASC, sd.id ASC
LIMIT @page_limit;

-- name: CreateSkillDistribution :one
INSERT INTO skill_distributions (
  project_id,
  skill_id,
  plugin_id,
  pinned_version_id,
  channel,
  created_by_user_id
)
SELECT
  s.project_id,
  s.id,
  @plugin_id::uuid,
  sqlc.narg(pinned_version_id)::uuid,
  'plugin',
  @created_by_user_id
FROM skills s
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
RETURNING *;

-- name: UpdateSkillDistribution :one
UPDATE skill_distributions
SET pinned_version_id = sqlc.narg(pinned_version_id)::uuid,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND plugin_id = @plugin_id
  AND channel = 'plugin'
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeActiveSkillDistribution :one
UPDATE skill_distributions
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND plugin_id = @plugin_id
  AND channel = 'plugin'
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeAllSkillDistributionsBySkill :many
-- The self-join returns the pre-revocation updated_at for audit snapshots.
UPDATE skill_distributions sd
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM skill_distributions prev
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
  ORDER BY (svo.origin IS DISTINCT FROM 'captured') DESC, sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE prev.id = sd.id
  AND sd.project_id = @project_id
  AND sd.skill_id = @skill_id
  AND sd.revoked_at IS NULL
RETURNING sqlc.embed(sd), prev.updated_at AS previous_updated_at, resolved.id AS resolved_version_id;

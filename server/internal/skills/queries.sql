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
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND sv.spec_valid IS TRUE
ORDER BY sv.created_at DESC, sv.id DESC
LIMIT 1;

-- name: GetActiveSkillDistributionRecord :one
SELECT sd.*
FROM skill_distributions sd
WHERE sd.project_id = @project_id
  AND sd.skill_id = @skill_id
  AND sd.channel = 'plugin'
  AND sd.revoked_at IS NULL;

-- name: ListActiveSkillDistributions :many
SELECT
  sqlc.embed(sd),
  resolved.id AS resolved_version_id
FROM skill_distributions sd
JOIN skills s
  ON s.project_id = sd.project_id
  AND s.id = sd.skill_id
  AND s.archived_at IS NULL
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
WHERE sd.project_id = @project_id
  AND sd.channel = 'plugin'
  AND sd.revoked_at IS NULL
ORDER BY sd.created_at ASC, sd.id ASC;

-- name: CreateSkillDistribution :one
INSERT INTO skill_distributions (
  project_id,
  skill_id,
  pinned_version_id,
  audience,
  channel,
  created_by_user_id
)
SELECT
  s.project_id,
  s.id,
  sqlc.narg(pinned_version_id)::uuid,
  sqlc.narg(audience)::text[],
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
    audience = sqlc.narg(audience)::text[],
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND channel = 'plugin'
  AND revoked_at IS NULL
RETURNING *;

-- name: RevokeActiveSkillDistribution :one
UPDATE skill_distributions
SET revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND skill_id = @skill_id
  AND channel = 'plugin'
  AND revoked_at IS NULL
RETURNING *;

-- name: ValidateSkillDistributionAudienceGroups :many
SELECT dg.workos_directory_group_id
FROM projects p
JOIN directory_groups dg ON dg.organization_id = p.organization_id
WHERE p.id = @project_id
  AND dg.workos_directory_group_id = ANY(@audience_group_ids::text[])
  AND dg.deleted IS FALSE
  AND dg.workos_deleted IS FALSE;

-- name: ListSkillDistributionAudienceGroups :many
SELECT
  dg.workos_directory_group_id,
  dg.name
FROM projects p
JOIN directory_groups dg ON dg.organization_id = p.organization_id
WHERE p.id = @project_id
  AND dg.deleted IS FALSE
  AND dg.workos_deleted IS FALSE
ORDER BY dg.name ASC, dg.workos_directory_group_id ASC;

-- name: GetActiveSkillDistributionStatus :one
SELECT
  sd.skill_id,
  resolved.id AS resolved_version_id,
  receipts.live,
  receipts.stale,
  receipts.shadowed,
  receipts.degraded
FROM skill_distributions sd
JOIN projects p ON p.id = sd.project_id
JOIN skills s
  ON s.project_id = sd.project_id
  AND s.id = sd.skill_id
  AND s.archived_at IS NULL
JOIN LATERAL (
  SELECT sv.id
  FROM skill_versions sv
  WHERE sv.skill_id = sd.skill_id
    AND sv.spec_valid IS TRUE
    AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id)
  ORDER BY sv.created_at DESC, sv.id DESC
  LIMIT 1
) resolved ON TRUE
JOIN LATERAL (
  SELECT
    COUNT(*) FILTER (WHERE ssr.status = 'applied' AND ssr.skill_version_id = resolved.id)::bigint AS live,
    COUNT(*) FILTER (WHERE ssr.status = 'applied' AND ssr.skill_version_id IS DISTINCT FROM resolved.id)::bigint AS stale,
    COUNT(*) FILTER (WHERE ssr.status = 'conflict_skipped')::bigint AS shadowed,
    COUNT(*) FILTER (WHERE ssr.status = 'fs_readonly')::bigint AS degraded
  FROM skill_sync_receipts ssr
  WHERE ssr.project_id = sd.project_id
    AND ssr.skill_id = sd.skill_id
    AND (
      sd.audience IS NULL
      OR EXISTS (
        SELECT 1
        FROM directory_users du
        JOIN directory_user_group_memberships m
          ON m.directory_user_id = du.id
          AND m.deleted IS FALSE
        JOIN directory_groups dg
          ON dg.id = m.directory_group_id
          AND dg.organization_id = p.organization_id
          AND dg.deleted IS FALSE
          AND dg.workos_deleted IS FALSE
        WHERE du.organization_id = p.organization_id
          AND du.user_id = ssr.user_id
          AND du.deleted IS FALSE
          AND du.workos_deleted IS FALSE
          AND dg.workos_directory_group_id = ANY(sd.audience)
      )
    )
) receipts ON TRUE
WHERE sd.project_id = @project_id
  AND sd.skill_id = @skill_id
  AND sd.channel = 'plugin'
  AND sd.revoked_at IS NULL;

-- name: UpsertSkillSyncReceipt :one
INSERT INTO skill_sync_receipts (
  project_id,
  skill_id,
  skill_version_id,
  user_id,
  hostname,
  provider,
  status
)
SELECT
  s.project_id,
  s.id,
  sqlc.narg(skill_version_id)::uuid,
  @user_id,
  @hostname,
  @provider,
  @status
FROM skills s
WHERE s.project_id = @project_id
  AND s.id = @skill_id
  AND s.archived_at IS NULL
  AND (
    sqlc.narg(skill_version_id)::uuid IS NULL
    OR EXISTS (
      SELECT 1
      FROM skill_versions sv
      WHERE sv.skill_id = s.id
        AND sv.id = sqlc.narg(skill_version_id)::uuid
        AND sv.spec_valid IS TRUE
    )
  )
FOR KEY SHARE OF s
ON CONFLICT (project_id, skill_id, user_id, hostname, provider) DO UPDATE SET
  skill_version_id = EXCLUDED.skill_version_id,
  status = EXCLUDED.status,
  synced_at = clock_timestamp(),
  updated_at = clock_timestamp()
RETURNING *;

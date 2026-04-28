-- name: CreateSkill :one
INSERT INTO skills (
    organization_id
  , project_id
  , name
  , slug
  , description
  , skill_uuid
  , active_version_id
  , created_by_user_id
)
VALUES (
    @organization_id
  , @project_id
  , @name
  , @slug
  , sqlc.narg(description)
  , sqlc.narg(skill_uuid)
  , sqlc.narg(active_version_id)
  , @created_by_user_id
)
RETURNING *;

-- name: GetSkill :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE;

-- name: GetSkillBySlug :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND slug = @slug
  AND deleted IS FALSE;

-- name: GetSkillBySkillUUID :one
SELECT *
FROM skills
WHERE project_id = @project_id
  AND skill_uuid = @skill_uuid
  AND deleted IS FALSE;

-- name: ListSkills :many
SELECT *
FROM skills
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListSkillsWithActiveVersion :many
WITH version_counts AS (
  SELECT
    skill_versions.skill_id,
    COUNT(*)::bigint AS version_count
  FROM skill_versions
  GROUP BY skill_versions.skill_id
)
SELECT
  sqlc.embed(skills),
  active_version.id AS active_version_id,
  active_version.content_sha256 AS active_version_content_sha256,
  active_version.asset_format AS active_version_asset_format,
  active_version.size_bytes AS active_version_size_bytes,
  active_version.author_name AS active_version_author_name,
  active_version.created_at AS active_version_created_at,
  active_version.first_seen_at AS active_version_first_seen_at,
  coalesce(version_counts.version_count, 0)::bigint AS version_count
FROM skills
LEFT JOIN skill_versions AS active_version
  ON active_version.id = skills.active_version_id
LEFT JOIN version_counts
  ON version_counts.skill_id = skills.id
WHERE skills.project_id = @project_id
  AND skills.deleted IS FALSE
ORDER BY skills.created_at DESC;

-- name: UpdateSkill :one
UPDATE skills
SET
    name = coalesce(sqlc.narg(name), name)
  , slug = coalesce(sqlc.narg(slug), slug)
  , description = coalesce(sqlc.narg(description), description)
  , skill_uuid = coalesce(sqlc.narg(skill_uuid), skill_uuid)
  , active_version_id = coalesce(sqlc.narg(active_version_id), active_version_id)
  , updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: ArchiveSkill :one
UPDATE skills
SET
    deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: CreateSkillVersion :one
WITH skill_lookup AS (
  SELECT skills.id AS skill_id
  FROM skills
  WHERE skills.id = @skill_id
    AND skills.project_id = @project_id
    AND skills.deleted IS FALSE
)
INSERT INTO skill_versions (
    skill_id
  , asset_id
  , content_sha256
  , asset_format
  , size_bytes
  , skill_bytes
  , state
  , captured_by_user_id
  , author_name
  , rejected_by_user_id
  , rejected_reason
  , rejected_at
  , first_seen_trace_id
  , first_seen_session_id
  , first_seen_at
)
SELECT
    skill_lookup.skill_id
  , @asset_id
  , @content_sha256
  , @asset_format
  , @size_bytes
  , sqlc.narg(skill_bytes)
  , @state
  , @captured_by_user_id
  , sqlc.narg(author_name)
  , sqlc.narg(rejected_by_user_id)
  , sqlc.narg(rejected_reason)
  , sqlc.narg(rejected_at)
  , sqlc.narg(first_seen_trace_id)
  , sqlc.narg(first_seen_session_id)
  , sqlc.narg(first_seen_at)
FROM skill_lookup
RETURNING *;

-- name: GetSkillVersion :one
SELECT sv.*
FROM skill_versions sv
INNER JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND sv.id = @id;

-- name: GetSkillVersionByHash :one
SELECT sv.*
FROM skill_versions sv
INNER JOIN skills s ON s.id = sv.skill_id
WHERE sv.skill_id = @skill_id
  AND sv.content_sha256 = @content_sha256
  AND s.project_id = @project_id;

-- name: ListSkillVersions :many
SELECT sv.*
FROM skill_versions sv
INNER JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND sv.skill_id = @skill_id
ORDER BY sv.created_at DESC;

-- name: ListPendingSkillVersions :many
SELECT
  sqlc.embed(s),
  sqlc.embed(sv)
FROM skill_versions sv
INNER JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.deleted IS FALSE
  AND sv.state = 'pending_review'
ORDER BY s.created_at DESC, sv.created_at DESC;

-- name: UpdateSkillVersionState :one
UPDATE skill_versions sv
SET
    state = @state
  , updated_at = clock_timestamp()
FROM skills s
WHERE sv.id = @id
  AND s.id = sv.skill_id
  AND s.project_id = @project_id
RETURNING sv.*;

-- name: RejectSkillVersion :one
UPDATE skill_versions sv
SET
    state = 'rejected'
  , rejected_by_user_id = @rejected_by_user_id
  , rejected_reason = sqlc.narg(rejected_reason)
  , rejected_at = clock_timestamp()
  , updated_at = clock_timestamp()
FROM skills s
WHERE sv.id = @id
  AND s.id = sv.skill_id
  AND s.project_id = @project_id
  AND sv.state = 'pending_review'
RETURNING sv.*;

-- name: SetSkillActiveVersion :one
UPDATE skills
SET
    active_version_id = @active_version_id
  , updated_at = clock_timestamp()
WHERE skills.project_id = @project_id
  AND skills.id = @id
  AND skills.deleted IS FALSE
  AND EXISTS (
    SELECT 1
    FROM skill_versions sv
    WHERE sv.id = @active_version_id
      AND sv.skill_id = skills.id
  )
RETURNING *;

-- name: ClearSkillActiveVersion :one
UPDATE skills
SET
    active_version_id = NULL
  , updated_at = clock_timestamp()
WHERE skills.project_id = @project_id
  AND skills.id = @id
  AND skills.deleted IS FALSE
RETURNING *;

-- name: SetSkillActiveVersionIfNull :one
UPDATE skills
SET
    active_version_id = @active_version_id
  , updated_at = clock_timestamp()
WHERE skills.project_id = @project_id
  AND skills.id = @id
  AND skills.deleted IS FALSE
  AND skills.active_version_id IS NULL
  AND EXISTS (
    SELECT 1
    FROM skill_versions sv
    WHERE sv.id = @active_version_id
      AND sv.skill_id = skills.id
  )
RETURNING *;

-- name: CreateSkillsCaptureAttempt :one
INSERT INTO skills_capture_attempts (
    organization_id
  , project_id
  , captured_by_user_id
  , skill_name
  , skill_slug
  , scope
  , discovery_root
  , source_type
  , resolution_status
  , content_sha256
  , asset_format
  , content_length
  , outcome
  , reason
  , skill_id
  , skill_version_id
  , asset_id
)
VALUES (
    @organization_id
  , @project_id
  , @captured_by_user_id
  , sqlc.narg(skill_name)
  , sqlc.narg(skill_slug)
  , @scope
  , @discovery_root
  , @source_type
  , @resolution_status
  , sqlc.narg(content_sha256)
  , sqlc.narg(asset_format)
  , sqlc.narg(content_length)
  , @outcome
  , @reason
  , sqlc.narg(skill_id)
  , sqlc.narg(skill_version_id)
  , sqlc.narg(asset_id)
)
RETURNING *;

-- name: ListSkillsCaptureAttempts :many
SELECT *
FROM skills_capture_attempts
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: ListSkillsCaptureAttemptsBySlug :many
SELECT *
FROM skills_capture_attempts
WHERE project_id = @project_id
  AND skill_slug = @skill_slug
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpsertOrganizationCapturePolicy :one
INSERT INTO skills_capture_policies (
    organization_id
  , project_id
  , mode
)
VALUES (
    @organization_id
  , NULL
  , @mode
)
ON CONFLICT (organization_id)
WHERE project_id IS NULL AND deleted IS FALSE
DO UPDATE
SET
    mode = EXCLUDED.mode
  , updated_at = clock_timestamp()
RETURNING *;

-- name: UpsertProjectCapturePolicyOverride :one
INSERT INTO skills_capture_policies (
    organization_id
  , project_id
  , mode
)
VALUES (
    @organization_id
  , sqlc.arg(project_id)::uuid
  , @mode
)
ON CONFLICT (organization_id, project_id)
WHERE project_id IS NOT NULL AND deleted IS FALSE
DO UPDATE
SET
    mode = EXCLUDED.mode
  , updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteProjectCapturePolicyOverride :one
UPDATE skills_capture_policies
SET
    deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = sqlc.arg(project_id)::uuid
  AND deleted IS FALSE
RETURNING *;

-- name: GetEffectiveCaptureMode :one
WITH project_override AS (
  SELECT scp.mode
  FROM skills_capture_policies scp
  WHERE scp.organization_id = @organization_id
    AND scp.project_id = @project_id
    AND scp.deleted IS FALSE
  LIMIT 1
),
org_default AS (
  SELECT scp.mode
  FROM skills_capture_policies scp
  WHERE scp.organization_id = @organization_id
    AND scp.project_id IS NULL
    AND scp.deleted IS FALSE
  LIMIT 1
)
SELECT
  coalesce(
    (SELECT mode FROM project_override),
    (SELECT mode FROM org_default),
    'disabled'
  )::text AS mode;

-- name: GetCaptureSettings :one
WITH project_override AS (
  SELECT scp.mode
  FROM skills_capture_policies scp
  WHERE scp.organization_id = @organization_id
    AND scp.project_id = @project_id
    AND scp.deleted IS FALSE
  LIMIT 1
),
org_default AS (
  SELECT scp.mode
  FROM skills_capture_policies scp
  WHERE scp.organization_id = @organization_id
    AND scp.project_id IS NULL
    AND scp.deleted IS FALSE
  LIMIT 1
)
SELECT
  coalesce((SELECT mode FROM org_default), '')::text AS org_default_mode,
  coalesce((SELECT mode FROM project_override), '')::text AS project_override_mode,
  coalesce(
    (SELECT mode FROM project_override),
    (SELECT mode FROM org_default),
    'disabled'
  )::text AS effective_mode;

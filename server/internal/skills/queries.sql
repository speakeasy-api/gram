-- name: CreateSkill :one
INSERT INTO skills (
    organization_id
  , project_id
  , name
  , slug
  , description
  , skill_uuid
  , state
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
  , @state
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

-- name: UpdateSkill :one
UPDATE skills
SET
    name = coalesce(sqlc.narg(name), name)
  , slug = coalesce(sqlc.narg(slug), slug)
  , description = coalesce(sqlc.narg(description), description)
  , skill_uuid = coalesce(sqlc.narg(skill_uuid), skill_uuid)
  , state = coalesce(sqlc.narg(state), state)
  , active_version_id = coalesce(sqlc.narg(active_version_id), active_version_id)
  , updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
RETURNING *;

-- name: ArchiveSkill :one
UPDATE skills
SET
    state = 'archived'
  , deleted_at = clock_timestamp()
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

-- name: SetSkillActiveVersion :one
UPDATE skills
SET
    active_version_id = @active_version_id
  , updated_at = clock_timestamp()
WHERE skills.project_id = @project_id
  AND skills.id = @id
  AND EXISTS (
    SELECT 1
    FROM skill_versions sv
    WHERE sv.id = @active_version_id
      AND sv.skill_id = skills.id
  )
RETURNING *;

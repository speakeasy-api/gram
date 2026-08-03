-- name: CreateLiteLLMInstance :one
INSERT INTO litellm_instances (
    organization_id
  , project_id
  , api_key_id
  , created_by_user_id
  , name
  , failure_posture
) VALUES (
    @organization_id
  , @project_id
  , @api_key_id
  , @created_by_user_id
  , @name
  , @failure_posture
)
RETURNING *;

-- name: ListLiteLLMInstances :many
SELECT
    li.id
  , li.organization_id
  , li.project_id
  , li.api_key_id
  , li.created_by_user_id
  , li.name
  , li.failure_posture
  , li.created_at
  , li.updated_at
  , p.name AS project_name
  , p.slug AS project_slug
  , ak.key_prefix
  , ak.last_accessed_at
  , (li.deleted IS FALSE AND ak.deleted IS FALSE) AS active
FROM litellm_instances li
JOIN projects p
  ON p.id = li.project_id
 AND p.organization_id = li.organization_id
JOIN api_keys ak
  ON ak.id = li.api_key_id
 AND ak.project_id = li.project_id
 AND ak.organization_id = li.organization_id
WHERE li.project_id = @project_id
  AND li.organization_id = @organization_id
ORDER BY li.created_at DESC;

-- name: GetLiteLLMInstanceForUpdate :one
SELECT *
FROM litellm_instances
WHERE id = @id
  AND project_id = @project_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
FOR UPDATE;

-- name: RotateLiteLLMInstanceKey :one
UPDATE litellm_instances
SET api_key_id = @new_api_key_id
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND organization_id = @organization_id
  AND api_key_id = @old_api_key_id
  AND deleted IS FALSE
RETURNING *;

-- name: RevokeLiteLLMInstance :one
UPDATE litellm_instances
SET deleted_at = clock_timestamp()
  , updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

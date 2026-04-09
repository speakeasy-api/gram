-- name: CreateTriggerInstance :one
INSERT INTO trigger_instances (
    organization_id,
    project_id,
    definition_slug,
    name,
    environment_id,
    target_kind,
    target_ref,
    target_display,
    config_json,
    status
) VALUES (
    @organization_id,
    @project_id,
    @definition_slug,
    @name,
    @environment_id,
    @target_kind,
    @target_ref,
    @target_display,
    @config_json,
    @status
) RETURNING *;

-- name: ListTriggerInstances :many
SELECT *
FROM trigger_instances ti
WHERE ti.project_id = @project_id
  AND ti.deleted IS FALSE
ORDER BY ti.created_at DESC;

-- name: GetTriggerInstanceByID :one
SELECT *
FROM trigger_instances ti
WHERE ti.id = @id
  AND ti.project_id = @project_id
  AND ti.deleted IS FALSE;

-- name: GetTriggerInstanceByIDPublic :one
SELECT *
FROM trigger_instances ti
WHERE ti.id = @id
  AND ti.deleted IS FALSE;

-- name: UpdateTriggerInstance :one
UPDATE trigger_instances
SET
    name = COALESCE(sqlc.narg('name'), name),
    environment_id = CASE
        WHEN @update_environment_id::boolean THEN sqlc.narg('environment_id')::uuid
        ELSE environment_id
    END,
    target_kind = COALESCE(sqlc.narg('target_kind'), target_kind),
    target_ref = COALESCE(sqlc.narg('target_ref'), target_ref),
    target_display = COALESCE(sqlc.narg('target_display'), target_display),
    config_json = COALESCE(sqlc.narg('config_json'), config_json),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: SetTriggerInstanceStatus :one
UPDATE trigger_instances
SET
    status = @status,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteTriggerInstance :one
UPDATE trigger_instances
SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

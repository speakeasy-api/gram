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

-- name: CreateDashboardTriggerInstance :one
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
)
ON CONFLICT (project_id, target_ref)
  WHERE definition_slug = 'dashboard' AND status = 'active' AND deleted IS FALSE
DO NOTHING
RETURNING *;

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

-- name: GetTriggerInstanceByIDForUpdate :one
SELECT *
FROM trigger_instances ti
WHERE ti.id = @id
  AND ti.project_id = @project_id
  AND ti.deleted IS FALSE
FOR UPDATE;

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

-- name: SetTriggerInstanceStatusByID :one
UPDATE trigger_instances
SET
    status = @status,
    updated_at = clock_timestamp()
WHERE id = @id
  AND status = @expected_status
  AND deleted IS FALSE
RETURNING *;

-- name: ListActiveTriggerInstancesByTarget :many
SELECT *
FROM trigger_instances ti
WHERE ti.project_id = @project_id
  AND ti.definition_slug = @definition_slug
  AND ti.target_kind = @target_kind
  AND ti.target_ref = @target_ref
  AND ti.status = 'active'
  AND ti.deleted IS FALSE;

-- name: ListTriggerEvents :many
SELECT
    e.id,
    e.trigger_instance_id,
    e.status,
    e.attempts,
    e.last_error,
    e.created_at,
    e.processed_at,
    t.chat_id
FROM assistant_thread_events e
LEFT JOIN assistant_threads t
    ON t.id = e.assistant_thread_id
    AND t.project_id = @project_id
    AND t.deleted IS FALSE
WHERE e.project_id = @project_id
  AND e.trigger_instance_id = @trigger_instance_id
  AND e.deleted IS FALSE
ORDER BY e.created_at DESC
LIMIT @row_limit;

-- name: DeleteTriggerInstance :one
UPDATE trigger_instances
SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteTriggerInstancesByTargetExceptDefinition :exec
UPDATE trigger_instances
SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND target_kind = @target_kind
  AND target_ref = @target_ref
  AND definition_slug <> @excluded_definition_slug
  AND deleted IS FALSE;

-- name: GetTriggerThreadRoute :one
SELECT *
FROM trigger_thread_routes
WHERE project_id = @project_id
  AND target_kind = @target_kind
  AND target_ref = @target_ref
  AND correlation_id = @correlation_id
  AND deleted IS FALSE;

-- name: UpsertTriggerThreadRouteState :one
-- Cursors are compared in byte order, so a cursor only moves forward when the
-- source's cursors sort lexically (Slack message timestamps do).
INSERT INTO trigger_thread_routes (
    project_id,
    target_kind,
    target_ref,
    correlation_id,
    state,
    last_seen_cursor
) VALUES (
    @project_id,
    @target_kind,
    @target_ref,
    @correlation_id,
    @state,
    sqlc.narg('last_seen_cursor')
)
ON CONFLICT (project_id, target_kind, target_ref, correlation_id) WHERE deleted IS FALSE
DO UPDATE SET
    state = EXCLUDED.state,
    last_seen_cursor = CASE
        WHEN EXCLUDED.last_seen_cursor IS NULL THEN trigger_thread_routes.last_seen_cursor
        WHEN trigger_thread_routes.last_seen_cursor IS NULL THEN EXCLUDED.last_seen_cursor
        WHEN EXCLUDED.last_seen_cursor COLLATE "C" > trigger_thread_routes.last_seen_cursor COLLATE "C" THEN EXCLUDED.last_seen_cursor
        ELSE trigger_thread_routes.last_seen_cursor
    END,
    updated_at = clock_timestamp()
RETURNING *;

-- name: RouteTriggerThread :one
INSERT INTO trigger_thread_routes (
    project_id,
    target_kind,
    target_ref,
    correlation_id,
    route_to_correlation_id,
    state,
    last_seen_cursor
) VALUES (
    @project_id,
    @target_kind,
    @target_ref,
    @correlation_id,
    @route_to_correlation_id,
    @state,
    sqlc.narg('last_seen_cursor')
)
ON CONFLICT (project_id, target_kind, target_ref, correlation_id) WHERE deleted IS FALSE
DO UPDATE SET
    route_to_correlation_id = EXCLUDED.route_to_correlation_id,
    state = EXCLUDED.state,
    last_seen_cursor = CASE
        WHEN EXCLUDED.last_seen_cursor IS NULL THEN trigger_thread_routes.last_seen_cursor
        WHEN trigger_thread_routes.last_seen_cursor IS NULL THEN EXCLUDED.last_seen_cursor
        WHEN EXCLUDED.last_seen_cursor COLLATE "C" > trigger_thread_routes.last_seen_cursor COLLATE "C" THEN EXCLUDED.last_seen_cursor
        ELSE trigger_thread_routes.last_seen_cursor
    END,
    updated_at = clock_timestamp()
RETURNING *;

-- name: SetTriggerThreadRoutesStateForTarget :execrows
-- Sets the state of every conversation delivered under @correlation_id: the
-- conversation itself and every conversation routed to it.
UPDATE trigger_thread_routes
SET state = @state,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND target_kind = @target_kind
  AND target_ref = @target_ref
  AND (correlation_id = @correlation_id OR route_to_correlation_id = @correlation_id)
  AND deleted IS FALSE;

-- name: AdvanceTriggerThreadRouteCursor :exec
-- Moves the cursor forward without touching state, so a delivery racing an
-- unsubscribe cannot subscribe the target again.
UPDATE trigger_thread_routes
SET last_seen_cursor = CASE
        WHEN last_seen_cursor IS NULL THEN sqlc.arg(last_seen_cursor)::text
        WHEN sqlc.arg(last_seen_cursor)::text COLLATE "C" > last_seen_cursor COLLATE "C" THEN sqlc.arg(last_seen_cursor)::text
        ELSE last_seen_cursor
    END,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND target_kind = @target_kind
  AND target_ref = @target_ref
  AND correlation_id = @correlation_id
  AND deleted IS FALSE;

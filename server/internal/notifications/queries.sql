-- name: CreateNotification :one
INSERT INTO notifications (
    project_id,
    type,
    level,
    title,
    message,
    actor_user_id,
    resource_type,
    resource_id
) VALUES (
    @project_id,
    @type,
    @level,
    @title,
    @message,
    @actor_user_id,
    @resource_type,
    @resource_id
) RETURNING *;

-- name: ListNotifications :many
SELECT *
FROM notifications
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (
    (@archived IS TRUE AND archived_at IS NOT NULL)
    OR (@archived IS FALSE AND archived_at IS NULL)
  )
ORDER BY created_at DESC
LIMIT @limit_count;

-- name: ListNotificationsByCursor :many
SELECT *
FROM notifications
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND id < @cursor
  AND (
    (@archived IS TRUE AND archived_at IS NOT NULL)
    OR (@archived IS FALSE AND archived_at IS NULL)
  )
ORDER BY created_at DESC
LIMIT @limit_count;

-- name: GetNotificationByID :one
SELECT *
FROM notifications
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: ArchiveNotification :one
UPDATE notifications
SET archived_at = now(), updated_at = now()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: CountNotificationsSince :one
SELECT COUNT(*)::integer
FROM notifications
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND archived_at IS NULL
  AND created_at > @since;

-- name: CountAllNotifications :one
SELECT COUNT(*)::integer
FROM notifications
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND archived_at IS NULL;

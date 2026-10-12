-- name: ListWidgets :many
SELECT *
FROM widgets
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY updated_at DESC, id DESC;

-- name: GetWidget :one
SELECT *
FROM widgets
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE;

-- name: GetWidgetForUpdate :one
-- Locks the row for the rest of the transaction, so a concurrent update or
-- delete waits and then sees the committed state: no lost update, no stale
-- audit snapshot, and a row deleted meanwhile reads as gone.
SELECT *
FROM widgets
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- name: CreateWidget :one
INSERT INTO widgets (
  project_id, organization_id, created_by_user_id, name, description, dataset, query, visualization
) VALUES (
  @project_id, @organization_id, sqlc.narg('created_by_user_id'), @name, sqlc.narg('description'), @dataset, @query::jsonb, @visualization::jsonb
)
RETURNING *;

-- name: UpdateWidget :one
UPDATE widgets
SET name = @name,
    description = sqlc.narg('description'),
    dataset = @dataset,
    query = @query::jsonb,
    visualization = @visualization::jsonb,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteWidget :one
UPDATE widgets
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: ListWidgetDashboards :many
-- Which live dashboards each of the project's widgets is on, once each,
-- however many cards show it.
SELECT DISTINCT p.widget_id, d.id AS dashboard_id, d.name AS dashboard_name
FROM dashboard_widgets p
JOIN dashboards d ON d.id = p.dashboard_id AND d.project_id = p.project_id AND d.deleted IS FALSE
WHERE p.project_id = @project_id
ORDER BY p.widget_id, d.name, d.id;

-- name: ListDashboardsForWidget :many
SELECT DISTINCT d.id AS dashboard_id, d.name AS dashboard_name
FROM dashboard_widgets p
JOIN dashboards d ON d.id = p.dashboard_id AND d.project_id = p.project_id AND d.deleted IS FALSE
WHERE p.project_id = @project_id
  AND p.widget_id = @widget_id
ORDER BY d.name, d.id;

-- name: DeleteWidgetPlacements :exec
-- A deleted widget comes off every dashboard it was on.
DELETE FROM dashboard_widgets
WHERE project_id = @project_id
  AND widget_id = @widget_id;

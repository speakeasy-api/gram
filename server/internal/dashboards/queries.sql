-- name: ListDashboards :many
SELECT *
FROM dashboards
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY updated_at DESC, id DESC;

-- name: GetDashboard :one
SELECT *
FROM dashboards
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE;

-- name: GetDashboardForUpdate :one
-- Locks the row for the rest of the transaction, so a concurrent edit or
-- delete waits and then sees the committed state, and a row deleted
-- meanwhile reads as gone.
SELECT *
FROM dashboards
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- name: CreateDashboard :one
INSERT INTO dashboards (
  project_id, organization_id, created_by_user_id, name, description, filters
) VALUES (
  @project_id, @organization_id, sqlc.narg('created_by_user_id'), @name, sqlc.narg('description'), @filters::jsonb
)
RETURNING *;

-- name: UpdateDashboard :one
UPDATE dashboards
SET name = @name,
    description = sqlc.narg('description'),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: UpdateDashboardFilters :one
UPDATE dashboards
SET filters = @filters::jsonb,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: TouchDashboard :one
-- A layout change is a change to the dashboard, so it moves up the list.
UPDATE dashboards
SET updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteDashboard :one
UPDATE dashboards
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: ListProjectPlacements :many
-- Every card on every live dashboard in the project, for the list. A card
-- whose widget was deleted is left out: there is nothing to draw.
SELECT p.*
FROM dashboard_widgets p
JOIN dashboards d ON d.id = p.dashboard_id AND d.deleted IS FALSE
JOIN widgets w ON w.id = p.widget_id AND w.deleted IS FALSE
WHERE p.project_id = @project_id
ORDER BY p.dashboard_id, p.y, p.x, p.id;

-- name: ListPlacements :many
SELECT p.*
FROM dashboard_widgets p
JOIN widgets w ON w.id = p.widget_id AND w.deleted IS FALSE
WHERE p.project_id = @project_id
  AND p.dashboard_id = @dashboard_id
ORDER BY p.y, p.x, p.id;

-- name: InsertPlacement :one
INSERT INTO dashboard_widgets (
  project_id, organization_id, dashboard_id, widget_id, x, y, w, h
) VALUES (
  @project_id, @organization_id, @dashboard_id, @widget_id, @x, @y, @w, @h
)
RETURNING *;

-- name: MovePlacement :one
UPDATE dashboard_widgets
SET x = @x,
    y = @y,
    w = @w,
    h = @h,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND dashboard_id = @dashboard_id
  AND id = @id
RETURNING *;

-- name: DeletePlacement :one
DELETE FROM dashboard_widgets
WHERE project_id = @project_id
  AND dashboard_id = @dashboard_id
  AND id = @id
RETURNING *;

-- name: GetWidgetForPlacement :one
-- The widget a card links to must be a live widget of the same project.
SELECT *
FROM widgets
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE;

-- name: ListWidgetsForDashboard :many
-- The widgets behind a dashboard's cards, once each, for duplicating them.
SELECT DISTINCT ON (w.id) w.*
FROM widgets w
JOIN dashboard_widgets p ON p.widget_id = w.id AND p.project_id = w.project_id
WHERE p.project_id = @project_id
  AND p.dashboard_id = @dashboard_id
  AND w.deleted IS FALSE
ORDER BY w.id;

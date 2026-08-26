-- name: ListExploreSavedQueries :many
SELECT *
FROM explore_saved_queries
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY updated_at DESC, id DESC;

-- name: GetExploreSavedQuery :one
SELECT *
FROM explore_saved_queries
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE;

-- name: LockExploreSavedQuery :one
SELECT *
FROM explore_saved_queries
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- name: CreateExploreSavedQuery :one
INSERT INTO explore_saved_queries (
  organization_id,
  name,
  chart_type,
  time_window,
  spec
)
VALUES (
  @organization_id,
  @name,
  @chart_type,
  @time_window,
  @spec::jsonb
)
RETURNING *;

-- name: UpdateExploreSavedQuery :one
UPDATE explore_saved_queries
SET name = @name,
    chart_type = @chart_type,
    time_window = @time_window,
    spec = @spec::jsonb,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteExploreSavedQuery :one
UPDATE explore_saved_queries
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

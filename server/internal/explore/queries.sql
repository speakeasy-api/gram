-- name: ListQueries :many
SELECT *
FROM queries
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY updated_at DESC, id DESC;

-- name: GetQuery :one
SELECT *
FROM queries
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE;

-- name: GetQueryForUpdate :one
-- Locks the row for the rest of the transaction, so a concurrent update or
-- delete waits and then sees the committed state: no lost update, no stale
-- audit snapshot, and a row deleted meanwhile reads as gone.
SELECT *
FROM queries
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- name: CreateQuery :one
INSERT INTO queries (
  project_id, organization_id, created_by_user_id, name, dataset, spec
) VALUES (
  @project_id, @organization_id, sqlc.narg('created_by_user_id'), @name, @dataset, @spec::jsonb
)
RETURNING *;

-- name: UpdateQuery :one
UPDATE queries
SET name = @name,
    dataset = @dataset,
    spec = @spec::jsonb,
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteQuery :one
UPDATE queries
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

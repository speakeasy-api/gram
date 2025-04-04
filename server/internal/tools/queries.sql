-- name: ListHttpToolDefinitions :many
SELECT *
FROM http_tool_definitions
WHERE project_id = @project_id 
  AND deleted IS FALSE
  AND (sqlc.narg(cursor)::uuid IS NULL OR id < sqlc.narg(cursor))
ORDER BY id DESC
LIMIT 100;

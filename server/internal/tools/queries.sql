-- name: ListAllHttpToolDefinitions :many
WITH deployment AS (
    SELECT id
    FROM deployments
    WHERE deployments.project_id = @project_id
      AND (
        sqlc.narg(deployment_id)::uuid IS NULL
        OR id = sqlc.narg(deployment_id)::uuid
      )
    ORDER BY seq DESC
    LIMIT 1
)
SELECT *
FROM http_tool_definitions
INNER JOIN deployment ON http_tool_definitions.deployment_id = deployment.id
WHERE http_tool_definitions.project_id = @project_id 
  AND http_tool_definitions.deleted IS FALSE
  AND (sqlc.narg(cursor)::uuid IS NULL OR http_tool_definitions.id < sqlc.narg(cursor))
ORDER BY http_tool_definitions.id DESC;

-- name: GetHTTPToolDefinitionByID :one
SELECT *
FROM http_tool_definitions
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;
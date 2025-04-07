-- name: ListAllHttpToolDefinitions :many
WITH latest_deployment AS (
    SELECT id, max(seq)
    FROM deployments
    WHERE project_id = @project_id
    GROUP BY id
)
SELECT *
FROM http_tool_definitions
INNER JOIN latest_deployment ON http_tool_definitions.deployment_id = latest_deployment.id
WHERE http_tool_definitions.project_id = @project_id 
  AND http_tool_definitions.deleted IS FALSE
  AND (sqlc.narg(cursor)::uuid IS NULL OR http_tool_definitions.id < sqlc.narg(cursor))
ORDER BY http_tool_definitions.id DESC
LIMIT 100;

-- name: ListDeploymentTools :many
SELECT *
FROM http_tool_definitions
WHERE deployment_id = @deployment_id;

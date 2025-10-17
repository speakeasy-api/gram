-- name: ListDeploymentHTTPTools :many
SELECT *
FROM http_tool_definitions
WHERE deployment_id = @deployment_id;

-- name: ListDeploymentFunctionsTools :many
SELECT *
FROM function_tool_definitions
WHERE deployment_id = @deployment_id;

-- name: CountFunctionsAccess :one
SELECT count(id)
FROM functions_access
WHERE
  project_id = @project_id
  AND deployment_id = @deployment_id;

-- name: ListDeploymentFunctionsResources :many
SELECT *
FROM function_resource_definitions
WHERE deployment_id = @deployment_id;

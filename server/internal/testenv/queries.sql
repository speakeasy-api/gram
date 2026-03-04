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

-- name: GetDeploymentTagByID :one
SELECT *
FROM deployment_tags
WHERE id = @id;

-- name: GetDeploymentTagByName :one
SELECT *
FROM deployment_tags
WHERE project_id = @project_id AND name = @name;

-- name: ListDeploymentTagHistoryByTagID :many
SELECT *
FROM deployment_tag_history
WHERE tag_id = @tag_id
ORDER BY changed_at DESC;

-- name: CreateTestDeployment :one
INSERT INTO deployments (
    idempotency_key
  , user_id
  , organization_id
  , project_id
) VALUES (
    @idempotency_key
  , @user_id
  , @organization_id
  , @project_id
)
RETURNING *;

-- name: ListFunctionResources :many
-- Two use cases:
-- 1. List all resources from the latest successful deployment (when deployment_id is NULL)
-- 2. List all resources for a specific deployment by ID (when deployment_id is provided)
WITH deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = @project_id
      AND (sqlc.narg(deployment_id)::uuid IS NOT NULL OR ds.status = 'completed')
      AND (
        sqlc.narg(deployment_id)::uuid IS NULL
        OR d.id = sqlc.narg(deployment_id)::uuid
      )
    ORDER BY d.seq DESC
    LIMIT 1
)
SELECT
  (SELECT id FROM deployment) as deployment_id,
  frd.id,
  frd.resource_urn,
  frd.name,
  frd.description,
  frd.uri,
  frd.title,
  frd.mime_type,
  frd.variables,
  frd.runtime,
  frd.function_id,
  frd.created_at,
  frd.updated_at
FROM function_resource_definitions frd
WHERE
  frd.deployment_id = (SELECT id FROM deployment)
  AND frd.deleted IS FALSE
  AND (sqlc.narg(cursor)::uuid IS NULL OR frd.id < sqlc.narg(cursor))
ORDER BY frd.id DESC
LIMIT $1;

-- name: FindFunctionResourcesByUrn :many
WITH deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = @project_id
    AND ds.status = 'completed'
    ORDER BY d.seq DESC
    LIMIT 1
)
SELECT
  sqlc.embed(function_resource_definitions),
  (select id from deployment) as owning_deployment_id
FROM function_resource_definitions
WHERE
  function_resource_definitions.deployment_id = (SELECT id FROM deployment)
  AND function_resource_definitions.deleted IS FALSE
  AND function_resource_definitions.resource_urn = ANY (@urns::text[])
ORDER BY function_resource_definitions.id DESC;

-- name: FindFunctionResourceEntriesByUrn :many
WITH deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = @project_id
    AND ds.status = 'completed'
    ORDER BY d.seq DESC
    LIMIT 1
)
SELECT
  frd.id, frd.resource_urn, frd.deployment_id, frd.name, frd.uri, frd.variables
FROM function_resource_definitions frd
WHERE
  frd.deployment_id = (SELECT id FROM deployment)
  AND frd.deleted IS FALSE
  AND frd.resource_urn = ANY (@urns::text[])
ORDER BY frd.id DESC;

-- name: GetFunctionResourceByURN :one
WITH deployment AS (
  SELECT d.id
  FROM deployments d
  JOIN deployment_statuses ds ON d.id = ds.deployment_id
  WHERE d.project_id = @project_id
  AND ds.status = 'completed'
  ORDER BY d.seq DESC LIMIT 1
)
SELECT
    resource.id
  , resource.resource_urn
  , resource.project_id
  , resource.deployment_id
  , resource.function_id
  , resource.runtime
  , resource.name
  , resource.description
  , resource.uri
  , resource.title
  , resource.mime_type
  , resource.variables
  , access.id AS access_id
FROM deployment dep
INNER JOIN function_resource_definitions resource
  ON resource.deployment_id = dep.id
  AND resource.resource_urn = @urn
  AND resource.project_id = @project_id
  AND resource.deleted IS FALSE
LEFT JOIN functions_access access
  ON access.project_id = @project_id
  AND access.deployment_id = dep.id
  AND access.function_id = resource.function_id
  AND access.deleted IS FALSE
ORDER BY access.seq DESC NULLS LAST
LIMIT 1;

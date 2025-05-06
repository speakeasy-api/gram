-- name: ListFirstPartyHTTPTools :many
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

-- name: ListTools :many
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
),
external_deployments AS (
  SELECT package_versions.deployment_id as id
  FROM deployments_packages
  INNER JOIN package_versions ON deployments_packages.version_id = package_versions.id
  WHERE deployments_packages.deployment_id = (SELECT id FROM deployment)
)
SELECT 
  (SELECT id FROM deployment) as deployment_id,
  http_tool_definitions.id,
  http_tool_definitions.name,
  http_tool_definitions.summary,
  http_tool_definitions.description,
  http_tool_definitions.http_method,
  http_tool_definitions.path,
  http_tool_definitions.openapiv3_document_id,
  http_tool_definitions.created_at,
  (CASE
    WHEN http_tool_definitions.project_id = @project_id THEN ''
    WHEN packages.id IS NOT NULL THEN packages.name
    ELSE ''
  END) as package_name
FROM http_tool_definitions
LEFT JOIN packages ON http_tool_definitions.project_id = packages.project_id
WHERE
  http_tool_definitions.deployment_id = ANY (SELECT id FROM deployment UNION ALL SELECT id FROM external_deployments)
  AND http_tool_definitions.deleted IS FALSE
  AND (sqlc.narg(cursor)::uuid IS NULL OR http_tool_definitions.id < sqlc.narg(cursor))
ORDER BY http_tool_definitions.id DESC;

-- name: FindToolsByName :many
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
),
external_deployments AS (
  SELECT package_versions.deployment_id as id
  FROM deployments_packages
  INNER JOIN package_versions ON deployments_packages.version_id = package_versions.id
  WHERE deployments_packages.deployment_id = (SELECT id FROM deployment)
)
SELECT 
  sqlc.embed(http_tool_definitions),
  (select id from deployment) as owning_deployment_id,
  (CASE
    WHEN http_tool_definitions.project_id = @project_id THEN ''
    WHEN packages.id IS NOT NULL THEN packages.name
    ELSE ''
  END) as package_name
FROM http_tool_definitions
LEFT JOIN packages ON http_tool_definitions.project_id = packages.project_id
WHERE
  http_tool_definitions.deployment_id = ANY (SELECT id FROM deployment UNION ALL SELECT id FROM external_deployments)
  AND http_tool_definitions.deleted IS FALSE
  AND http_tool_definitions.name = ANY (@names::text[])
ORDER BY http_tool_definitions.id DESC;

-- name: GetHTTPToolDefinitionByID :one
SELECT *
FROM http_tool_definitions
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;
-- name: ListHttpTools :many
-- Two use cases:
-- 1. List all tools from the latest successful deployment (when deployment_id is NULL)
-- 2. List all tools for a specific deployment by ID (when deployment_id is provided)
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
),
all_deployment_ids AS (
    SELECT id FROM deployment
    UNION
    SELECT DISTINCT pv.deployment_id
    FROM deployment d
    JOIN deployments_packages dp ON dp.deployment_id = d.id
    JOIN package_versions pv ON dp.version_id = pv.id
)
SELECT
  (SELECT id FROM deployment) as deployment_id,
  htd.id,
  htd.tool_urn,
  htd.name,
  htd.summary,
  htd.description,
  htd.http_method,
  htd.confirm,
  htd.confirm_prompt,
  htd.summarizer,
  htd.response_filter,
  htd.path,
  doa.asset_id,
  htd.openapiv3_document_id,
  htd.openapiv3_operation,
  htd.schema_version,
  htd.schema,
  htd.security,
  htd.default_server_url,
  htd.created_at,
  htd.updated_at,
  htd.tags,
  htd.read_only_hint,
  htd.destructive_hint,
  htd.idempotent_hint,
  htd.open_world_hint,
  (CASE
    WHEN htd.project_id = @project_id THEN ''
    WHEN packages.id IS NOT NULL THEN packages.name
    ELSE ''
  END) as package_name
FROM http_tool_definitions htd
LEFT JOIN packages ON htd.project_id = packages.project_id
LEFT JOIN deployments_openapiv3_assets doa ON htd.openapiv3_document_id = doa.id
WHERE
  htd.deployment_id IN (SELECT id FROM all_deployment_ids)
  AND htd.deleted IS FALSE
  AND (sqlc.narg(cursor)::uuid IS NULL OR htd.id < sqlc.narg(cursor))
  AND (sqlc.narg(urn_prefix)::text IS NULL OR htd.tool_urn LIKE sqlc.narg(urn_prefix) || '%' ESCAPE '\')
ORDER BY htd.id DESC
LIMIT $1;

-- name: FindHttpToolsByUrn :many
WITH deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = @project_id
    AND ds.status = 'completed'
    ORDER BY d.seq DESC
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
  doa.asset_id,
  (CASE
    WHEN http_tool_definitions.project_id = @project_id THEN ''
    WHEN packages.id IS NOT NULL THEN packages.name
    ELSE ''
  END) as package_name
FROM http_tool_definitions
LEFT JOIN packages ON http_tool_definitions.project_id = packages.project_id
LEFT JOIN deployments_openapiv3_assets doa ON http_tool_definitions.openapiv3_document_id = doa.id
WHERE
  http_tool_definitions.deployment_id = ANY (SELECT id FROM deployment UNION ALL SELECT id FROM external_deployments)
  AND http_tool_definitions.deleted IS FALSE
  AND http_tool_definitions.tool_urn = ANY (@urns::text[])
ORDER BY http_tool_definitions.id DESC;

-- name: FindHttpToolEntriesByUrn :many
WITH deployment AS (
    SELECT d.id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = @project_id
    AND ds.status = 'completed'
    ORDER BY d.seq DESC
    LIMIT 1
),
external_deployments AS (
  SELECT package_versions.deployment_id as id
  FROM deployments_packages
  INNER JOIN package_versions ON deployments_packages.version_id = package_versions.id
  WHERE deployments_packages.deployment_id = (SELECT id FROM deployment)
)
SELECT
  htd.id, htd.tool_urn, htd.deployment_id, htd.openapiv3_document_id, htd.name, htd.security, htd.server_env_var,
  htd.read_only_hint, htd.destructive_hint, htd.idempotent_hint, htd.open_world_hint,
  htd.http_method
FROM http_tool_definitions htd
WHERE
  htd.deployment_id = ANY (SELECT id FROM deployment UNION ALL SELECT id FROM external_deployments)
  AND htd.deleted IS FALSE
  AND htd.tool_urn = ANY (@urns::text[])
ORDER BY htd.id DESC;

-- name: GetHTTPToolDefinitionByURN :one
WITH deployment AS (
  SELECT d.id 
  FROM deployments d
  JOIN deployment_statuses ds ON d.id = ds.deployment_id
  WHERE d.project_id = @project_id
  AND ds.status = 'completed'
  ORDER BY d.seq DESC LIMIT 1
)
SELECT *
FROM http_tool_definitions
WHERE http_tool_definitions.tool_urn = @urn
  AND http_tool_definitions.project_id = @project_id
  AND http_tool_definitions.deleted IS FALSE 
  AND http_tool_definitions.deployment_id = (SELECT id FROM deployment)
LIMIT 1;

-- name: ListFunctionTools :many
-- Two use cases:
-- 1. List all tools from the latest successful deployment (when deployment_id is NULL)
-- 2. List all tools for a specific deployment by ID (when deployment_id is provided)
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
  ftd.id,
  ftd.tool_urn,
  ftd.name,
  ftd.description,
  ftd.input_schema,
  ftd.variables,
  ftd.auth_input,
  ftd.runtime,
  ftd.function_id,
  ftd.meta,
  ftd.read_only_hint,
  ftd.destructive_hint,
  ftd.idempotent_hint,
  ftd.open_world_hint,
  df.asset_id,
  ftd.created_at,
  ftd.updated_at
FROM function_tool_definitions ftd
LEFT JOIN deployments_functions df ON ftd.function_id = df.id
WHERE
  ftd.deployment_id = (SELECT id FROM deployment)
  AND ftd.deleted IS FALSE
  AND (sqlc.narg(cursor)::uuid IS NULL OR ftd.id < sqlc.narg(cursor))
  AND (sqlc.narg(urn_prefix)::text IS NULL OR ftd.tool_urn LIKE sqlc.narg(urn_prefix) || '%' ESCAPE '\')
ORDER BY ftd.id DESC
LIMIT $1;

-- name: FindFunctionToolsByUrn :many
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
  sqlc.embed(ftd),
  (select id from deployment) as owning_deployment_id,
  df.asset_id
FROM function_tool_definitions ftd
LEFT JOIN deployments_functions df ON ftd.function_id = df.id
WHERE
  ftd.deployment_id = (SELECT id FROM deployment)
  AND ftd.deleted IS FALSE
  AND ftd.tool_urn = ANY (@urns::text[])
ORDER BY ftd.id DESC;

-- name: FindFunctionToolEntriesByUrn :many
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
  ftd.id, ftd.tool_urn, ftd.deployment_id, ftd.name, ftd.variables, ftd.auth_input,
  ftd.read_only_hint, ftd.destructive_hint, ftd.idempotent_hint, ftd.open_world_hint
FROM function_tool_definitions ftd
WHERE
  ftd.deployment_id = (SELECT id FROM deployment)
  AND ftd.deleted IS FALSE
  AND ftd.tool_urn = ANY (@urns::text[])
ORDER BY ftd.id DESC;

-- name: GetFunctionToolByURN :one
WITH deployment AS (
  SELECT d.id
  FROM deployments d
  JOIN deployment_statuses ds ON d.id = ds.deployment_id
  WHERE d.project_id = @project_id
  AND ds.status = 'completed'
  ORDER BY d.seq DESC LIMIT 1
)
SELECT
    tool.id
  , tool.tool_urn
  , tool.project_id
  , tool.deployment_id
  , tool.function_id
  , tool.runtime
  , tool.name
  , tool.description
  , tool.input_schema
  , tool.variables
  , tool.auth_input
  , tool.meta
  , access.id AS access_id
FROM deployment dep
INNER JOIN function_tool_definitions tool
  ON tool.deployment_id = dep.id
  AND tool.tool_urn = @urn
  AND tool.project_id = @project_id
  AND tool.deleted IS FALSE
LEFT JOIN functions_access access
  ON access.project_id = @project_id
  AND access.deployment_id = dep.id
  AND access.function_id = tool.function_id
  AND access.deleted IS FALSE
ORDER BY access.seq DESC NULLS LAST
LIMIT 1;

-- name: FindHttpToolEntriesForProjects :many
-- Batch-resolves HTTP tool entries across multiple projects in a single query.
-- Resolves each project's latest completed deployment once, including package deployments.
-- No URN filter — caller filters in Go against toolset version URN sets.
WITH project_deployments AS (
    SELECT DISTINCT ON (d.project_id) d.project_id, d.id as deployment_id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = ANY(@project_ids::uuid[])
      AND ds.status = 'completed'
    ORDER BY d.project_id, d.seq DESC
),
all_deployment_ids AS (
    SELECT project_id, deployment_id as id FROM project_deployments
    UNION
    SELECT pd.project_id, pv.deployment_id as id
    FROM project_deployments pd
    JOIN deployments_packages dp ON dp.deployment_id = pd.deployment_id
    JOIN package_versions pv ON dp.version_id = pv.id
)
SELECT
  adi.project_id,
  htd.id,
  htd.tool_urn,
  htd.name,
  htd.read_only_hint,
  htd.destructive_hint,
  htd.idempotent_hint,
  htd.open_world_hint,
  htd.http_method
FROM http_tool_definitions htd
INNER JOIN all_deployment_ids adi ON htd.deployment_id = adi.id
WHERE htd.deleted IS FALSE;

-- name: FindFunctionToolEntriesForProjects :many
-- Batch-resolves function tool entries across multiple projects in a single query.
WITH project_deployments AS (
    SELECT DISTINCT ON (d.project_id) d.project_id, d.id as deployment_id
    FROM deployments d
    JOIN deployment_statuses ds ON d.id = ds.deployment_id
    WHERE d.project_id = ANY(@project_ids::uuid[])
      AND ds.status = 'completed'
    ORDER BY d.project_id, d.seq DESC
)
SELECT
  pd.project_id,
  ftd.id,
  ftd.tool_urn,
  ftd.name,
  ftd.read_only_hint,
  ftd.destructive_hint,
  ftd.idempotent_hint,
  ftd.open_world_hint
FROM function_tool_definitions ftd
INNER JOIN project_deployments pd ON ftd.deployment_id = pd.deployment_id
WHERE ftd.deleted IS FALSE;

-- name: PokeToolDefinitionByUrn :one
WITH first_party AS (
  SELECT id
  FROM http_tool_definitions
  WHERE http_tool_definitions.tool_urn = @urn
    AND http_tool_definitions.project_id = @project_id
    AND http_tool_definitions.deleted IS FALSE
  LIMIT 1
),
-- This CTE is for integrating third party tools by checking for tool definitions from external deployments/packages.
third_party AS (
  SELECT htd.id
  FROM deployments d
  INNER JOIN deployments_packages dp ON d.id =  dp.deployment_id
  INNER JOIN package_versions pv ON dp.version_id = pv.id
  INNER JOIN http_tool_definitions htd ON htd.deployment_id = pv.deployment_id
  WHERE d.project_id = @project_id
    AND htd.tool_urn = @urn
    AND NOT EXISTS(SELECT 1 FROM first_party)
  LIMIT 1
),
function_tools AS (
  SELECT id
  FROM function_tool_definitions
  WHERE function_tool_definitions.tool_urn = @urn
    AND function_tool_definitions.project_id = @project_id
    AND function_tool_definitions.deleted IS FALSE
    AND NOT EXISTS(SELECT 1 FROM first_party)
    AND NOT EXISTS(SELECT 1 FROM third_party)
  LIMIT 1
)
SELECT
  COALESCE(
    (SELECT id FROM first_party),
    (SELECT id FROM third_party),
    (SELECT id FROM function_tools)
  ) AS id;

-- name: CreateHTTPToolDefinition :exec
-- Inserts an http_tool_definition row. The production tool-ingestion path
-- lives in the deployments service.
INSERT INTO http_tool_definitions (
    project_id
  , deployment_id
  , tool_urn
  , name
  , untruncated_name
  , summary
  , description
  , tags
  , http_method
  , path
  , schema_version
  , schema
  , server_env_var
  , security
  , header_settings
  , query_settings
  , path_settings
  , read_only_hint
  , destructive_hint
  , idempotent_hint
  , open_world_hint
) VALUES (
    @project_id
  , @deployment_id
  , @tool_urn
  , @name
  , @untruncated_name
  , @summary
  , @description
  , @tags
  , @http_method
  , @path
  , @schema_version
  , @schema
  , @server_env_var
  , @security
  , @header_settings
  , @query_settings
  , @path_settings
  , @read_only_hint
  , @destructive_hint
  , @idempotent_hint
  , @open_world_hint
);

-- name: GetDeployment :one
WITH latest_status as (
    SELECT deployment_id, status
    FROM deployment_statuses
    WHERE deployment_id = @id
    ORDER BY seq DESC
    LIMIT 1
)
SELECT sqlc.embed(deployments), coalesce(latest_status.status, 'unknown') as status
FROM deployments
LEFT JOIN latest_status ON deployments.id = latest_status.deployment_id
WHERE deployments.id = @id AND deployments.project_id = @project_id;

-- name: ListDeployments :many
WITH latest_statuses AS (
  SELECT DISTINCT ON (deployment_id) deployment_id, status
  FROM deployment_statuses
  WHERE deployment_id IN (
    SELECT id FROM deployments WHERE project_id = @project_id
  )
  ORDER BY deployment_id, seq DESC
)
SELECT 
  d.id,
  d.user_id,
  d.created_at,
  COALESCE(ls.status, 'unknown') as status,
  COUNT(DISTINCT doa.id) as openapiv3_asset_count,
  COUNT(DISTINCT htd.id) as openapiv3_tool_count,
  COUNT(DISTINCT tf.function_id) as functions_asset_count,
  COUNT(DISTINCT tf.id) as functions_tool_count,
  COUNT(DISTINCT ema.id) as external_mcp_asset_count,
  COUNT(DISTINCT emtd.id) as external_mcp_tool_count
FROM deployments d
LEFT JOIN latest_statuses ls ON d.id = ls.deployment_id
LEFT JOIN deployments_openapiv3_assets doa ON d.id = doa.deployment_id
LEFT JOIN http_tool_definitions htd ON d.id = htd.deployment_id AND htd.deleted IS FALSE
LEFT JOIN function_tool_definitions tf ON d.id = tf.deployment_id AND tf.deleted IS FALSE
LEFT JOIN external_mcp_attachments ema ON d.id = ema.deployment_id AND ema.deleted IS FALSE
LEFT JOIN external_mcp_tool_definitions emtd ON ema.id = emtd.external_mcp_attachment_id AND emtd.deleted IS FALSE
WHERE
  d.project_id = @project_id
  AND d.id <= CASE 
    WHEN sqlc.narg(cursor)::uuid IS NOT NULL THEN sqlc.narg(cursor)::uuid
    ELSE (SELECT id FROM deployments WHERE project_id = @project_id ORDER BY id DESC LIMIT 1)
  END
GROUP BY d.id, ls.status
ORDER BY d.id DESC
LIMIT 51;

-- name: GetDeploymentLogs :many
WITH latest_status as (
    SELECT s.status
    FROM deployment_statuses s
    WHERE s.deployment_id = @deployment_id
    ORDER BY s.seq DESC
    LIMIT 1
)
SELECT
  coalesce((select status from latest_status), 'unknown')::text as status,
  log.id,
  log.seq,
  log.event,
  log.message,
  log.attachment_id,
  log.attachment_type,
  log.created_at
FROM deployment_logs log
WHERE
  log.deployment_id = @deployment_id AND log.project_id = @project_id
  AND log.seq >= CASE
    WHEN sqlc.narg(cursor_seq)::int8 IS NOT NULL THEN sqlc.narg(cursor_seq)::int8
    ELSE (
      SELECT dl.seq
      FROM deployment_logs dl
      WHERE dl.deployment_id = @deployment_id AND dl.project_id = @project_id
      ORDER BY dl.seq ASC LIMIT 1
    )
  END
ORDER BY log.seq ASC
LIMIT 51;

-- name: GetDeploymentWithAssets :many
WITH latest_status as (
    SELECT deployment_id, status
    FROM deployment_statuses
    WHERE deployment_id = @id
    ORDER BY seq DESC
    LIMIT 1
),
openapiv3_tool_counts as (
    SELECT
        deployment_id,
        COUNT(DISTINCT id) as tool_count
    FROM http_tool_definitions
    WHERE deployment_id = @id AND deleted IS FALSE
    GROUP BY deployment_id
),
functions_tool_counts as (
    SELECT
        deployment_id,
        COUNT(DISTINCT id) as tool_count
    FROM function_tool_definitions
    WHERE deployment_id = @id AND deleted IS FALSE
    GROUP BY deployment_id
),
external_mcp_tool_counts as (
    SELECT
        ema.deployment_id,
        COUNT(DISTINCT emtd.id) as tool_count
    FROM external_mcp_tool_definitions emtd
    JOIN external_mcp_attachments ema ON emtd.external_mcp_attachment_id = ema.id
    WHERE ema.deployment_id = @id AND emtd.deleted IS FALSE AND ema.deleted IS FALSE
    GROUP BY ema.deployment_id
)
SELECT
  sqlc.embed(deployments),
  coalesce(latest_status.status, 'unknown') as status,
  deployments_openapiv3_assets.id as deployments_openapiv3_asset_id,
  deployments_openapiv3_assets.asset_id as deployments_openapiv3_asset_store_id,
  deployments_openapiv3_assets.name as deployments_openapiv3_asset_name,
  deployments_openapiv3_assets.slug as deployments_openapiv3_asset_slug,
  deployments_functions.id as deployments_functions_id,
  deployments_functions.asset_id as deployments_functions_asset_id,
  deployments_functions.name as deployments_functions_name,
  deployments_functions.slug as deployments_functions_slug,
  deployments_functions.runtime as deployments_functions_runtime,
  deployments_packages.package_id as deployment_package_id,
  packages.name as package_name,
  package_versions.major as package_version_major,
  package_versions.minor as package_version_minor,
  package_versions.patch as package_version_patch,
  package_versions.prerelease as package_version_prerelease,
  package_versions.build as package_version_build,
  COALESCE(openapiv3_tool_counts.tool_count, 0) as openapiv3_tool_count,
  COALESCE(functions_tool_counts.tool_count, 0) as functions_tool_count,
  COALESCE(external_mcp_tool_counts.tool_count, 0) as external_mcp_tool_count,
  external_mcp_attachments.id as external_mcp_id,
  external_mcp_attachments.registry_id as external_mcp_registry_id,
  external_mcp_attachments.name as external_mcp_name,
  external_mcp_attachments.slug as external_mcp_slug,
  external_mcp_attachments.registry_server_specifier as external_mcp_registry_server_specifier
FROM deployments
LEFT JOIN deployments_openapiv3_assets ON deployments.id = deployments_openapiv3_assets.deployment_id
LEFT JOIN deployments_functions ON deployments.id = deployments_functions.deployment_id
LEFT JOIN deployments_packages ON deployments.id = deployments_packages.deployment_id
LEFT JOIN latest_status ON deployments.id = latest_status.deployment_id
LEFT JOIN packages ON deployments_packages.package_id = packages.id
LEFT JOIN package_versions ON deployments_packages.version_id = package_versions.id
LEFT JOIN openapiv3_tool_counts ON deployments.id = openapiv3_tool_counts.deployment_id
LEFT JOIN functions_tool_counts ON deployments.id = functions_tool_counts.deployment_id
LEFT JOIN external_mcp_tool_counts ON deployments.id = external_mcp_tool_counts.deployment_id
LEFT JOIN external_mcp_attachments ON deployments.id = external_mcp_attachments.deployment_id AND external_mcp_attachments.deleted IS FALSE
WHERE deployments.id = @id AND deployments.project_id = @project_id;

-- name: GetLatestDeploymentID :one
SELECT id
FROM deployments
WHERE project_id = @project_id
ORDER BY id DESC
LIMIT 1;

-- name: GetActiveDeploymentID :one
SELECT d.id
FROM deployments d
INNER JOIN deployment_statuses ds
ON d.id = ds.deployment_id
WHERE d.project_id = @project_id
AND ds.status = 'completed'
ORDER BY d.id DESC
LIMIT 1;

-- name: GetDeploymentByIdempotencyKey :one
WITH latest_status as (
    SELECT deployment_id, status
    FROM deployment_statuses
    INNER JOIN deployments ON deployment_statuses.deployment_id = deployments.id
    WHERE deployments.idempotency_key = @idempotency_key
    ORDER BY deployment_statuses.seq DESC
    LIMIT 1
)
SELECT sqlc.embed(deployments), coalesce(latest_status.status, 'unknown') as status
FROM deployments
LEFT JOIN latest_status ON deployments.id = latest_status.deployment_id
WHERE deployments.idempotency_key = @idempotency_key
 AND deployments.project_id = @project_id;

-- name: GetDeploymentOpenAPIv3 :many
SELECT *
FROM deployments_openapiv3_assets
WHERE deployment_id = @deployment_id;

-- name: CreateDeployment :execresult
INSERT INTO deployments (
  idempotency_key
  , user_id
  , organization_id
  , project_id
  , github_repo
  , github_pr
  , github_sha
  , external_id
  , external_url
) VALUES (
  @idempotency_key
  , @user_id
  , @organization_id
  , @project_id
  , @github_repo
  , @github_pr
  , @github_sha
  , @external_id
  , @external_url
)
ON CONFLICT (project_id, idempotency_key) DO NOTHING;

-- name: CloneDeployment :one
INSERT INTO deployments (
  cloned_from
  , idempotency_key
  , user_id
  , organization_id
  , project_id
  , github_repo
  , github_pr
  , github_sha
  , external_id
  , external_url
)
SELECT
  current.id
  , gen_random_uuid()
  , current.user_id
  , current.organization_id
  , current.project_id
  , current.github_repo
  , current.github_pr
  , current.github_sha
  , current.external_id
  , current.external_url
FROM deployments as current
WHERE current.id = @id AND current.project_id = @project_id
RETURNING id;

-- name: CloneDeploymentPackages :many
INSERT INTO deployments_packages (
  deployment_id
  , package_id
  , version_id
)
SELECT 
  @clone_deployment_id
  , current.package_id
  , current.version_id
FROM deployments_packages as current
WHERE current.deployment_id = @original_deployment_id
  AND current.package_id <> ALL (@excluded_ids::uuid[])
RETURNING id, package_id, version_id;

-- name: CloneDeploymentOpenAPIv3Assets :many
INSERT INTO deployments_openapiv3_assets (
  deployment_id
  , asset_id
  , name
  , slug
)
SELECT 
  @clone_deployment_id
  , current.asset_id
  , current.name
  , current.slug
FROM deployments_openapiv3_assets as current
WHERE current.deployment_id = @original_deployment_id
  AND current.asset_id <> ALL (@excluded_ids::uuid[])
RETURNING id;

-- name: CloneDeploymentFunctionsAssets :many
INSERT INTO deployments_functions (
  deployment_id
  , asset_id
  , name
  , slug
  , runtime
)
SELECT 
  @clone_deployment_id
  , current.asset_id
  , current.name
  , current.slug
  , current.runtime
FROM deployments_functions as current
WHERE current.deployment_id = @original_deployment_id
  AND current.asset_id <> ALL (@excluded_ids::uuid[])
RETURNING id;

-- name: CloneDeploymentToolFunctions :many
INSERT INTO function_tool_definitions (
  deployment_id
  , function_id
  , tool_urn
  , project_id
  , name
  , description
  , runtime
  , variables
  , auth_input
  , input_schema
  , read_only_hint
  , destructive_hint
  , idempotent_hint
  , open_world_hint
)
SELECT
  @clone_deployment_id
  , current.function_id
  , current.tool_urn
  , current.project_id
  , current.name
  , current.description
  , current.runtime
  , current.variables
  , current.auth_input
  , current.input_schema
  , current.read_only_hint
  , current.destructive_hint
  , current.idempotent_hint
  , current.open_world_hint
FROM function_tool_definitions as current
WHERE current.deployment_id = @original_deployment_id
  AND current.name <> ALL (@excluded_names::text[])
RETURNING id;

-- name: TransitionDeployment :one
WITH current_status AS (
  SELECT 0 as state, id, deployment_id, status
  FROM deployment_statuses as d
  WHERE d.deployment_id = @deployment_id
  ORDER BY d.seq DESC
  LIMIT 1
),
status_update AS (
  INSERT INTO deployment_statuses (deployment_id, status)
  SELECT @deployment_id, @status
  WHERE (
    CASE
      WHEN @status = 'created' THEN NOT EXISTS (SELECT 1 FROM current_status)
      WHEN @status = 'pending' THEN EXISTS (SELECT 1 FROM current_status WHERE status = 'created')
      WHEN @status = 'failed' THEN EXISTS (SELECT 1 FROM current_status WHERE status = 'pending')
      WHEN @status = 'completed' THEN EXISTS (SELECT 1 FROM current_status WHERE status = 'pending')
      ELSE FALSE
    END
  )
  LIMIT 1
  RETURNING 1 as state, id, deployment_id, status
),
new_log AS (
  INSERT INTO deployment_logs (deployment_id, project_id, event, message)
  SELECT @deployment_id, @project_id, @event, @message
  WHERE EXISTS (SELECT 1 FROM status_update)
  RETURNING id
),
all_statuses AS (
  SELECT * FROM status_update
  UNION ALL
  SELECT * FROM current_status
)
SELECT 
    all_statuses.id as status_id
  , all_statuses.status as status
  , (CASE 
      WHEN all_statuses.state = 1 THEN TRUE
      ELSE FALSE
    END) as moved
FROM all_statuses
ORDER BY all_statuses.state DESC
LIMIT 1;

-- name: LogDeploymentEvent :exec
INSERT INTO deployment_logs (deployment_id, project_id, event, message, attachment_id, attachment_type)
VALUES (@deployment_id, @project_id, @event, @message, sqlc.narg(attachment_id), sqlc.narg(attachment_type));

-- name: BatchLogEvents :copyfrom
INSERT INTO deployment_logs (deployment_id, project_id, event, message, attachment_id, attachment_type)
VALUES (@deployment_id, @project_id, @event, @message, sqlc.narg(attachment_id), sqlc.narg(attachment_type));

-- name: UpsertDeploymentOpenAPIv3Asset :one
INSERT INTO deployments_openapiv3_assets (
  deployment_id,
  asset_id,
  name,
  slug
) VALUES (
  @deployment_id,
  @asset_id,
  @name,
  @slug
)
ON CONFLICT (deployment_id, slug) DO UPDATE
SET
  asset_id = EXCLUDED.asset_id,
  name = EXCLUDED.name
RETURNING id, asset_id, name, slug;

-- name: UpsertDeploymentFunctionsAsset :one
INSERT INTO deployments_functions (
  deployment_id
  , asset_id
  , name
  , slug
  , runtime
) VALUES (
  @deployment_id
  , @asset_id
  , @name
  , @slug
  , @runtime
)
ON CONFLICT (deployment_id, slug) DO UPDATE
SET
  asset_id = EXCLUDED.asset_id
  , name = EXCLUDED.name
  , runtime = EXCLUDED.runtime
RETURNING id, asset_id, name, slug;

-- name: UpsertDeploymentPackage :one
INSERT INTO deployments_packages (
  deployment_id
  , package_id
  , version_id
) VALUES (
  @deployment_id,
  @package_id,
  @version_id
)
ON CONFLICT (deployment_id, package_id) DO UPDATE
SET
  version_id = EXCLUDED.version_id
RETURNING id;

-- name: CreateOpenAPIv3ToolDefinition :one
INSERT INTO http_tool_definitions (
    project_id
  , deployment_id
  , openapiv3_document_id
  , tool_urn
  , name
  , untruncated_name
  , openapiv3_operation
  , summary
  , description
  , tags
  , confirm
  , confirm_prompt
  , x_gram
  , original_name
  , original_summary
  , original_description
  , security
  , http_method
  , path
  , schema_version
  , schema
  , header_settings
  , query_settings
  , path_settings
  , server_env_var
  , default_server_url
  , request_content_type
  , response_filter
  , read_only_hint
  , destructive_hint
  , idempotent_hint
  , open_world_hint
) VALUES (
    @project_id
  , @deployment_id
  , @openapiv3_document_id
  , @tool_urn
  , @name
  , @untruncated_name
  , @openapiv3_operation
  , @summary
  , @description
  , @tags
  , @confirm
  , @confirm_prompt
  , @x_gram
  , @original_name
  , @original_summary
  , @original_description
  , @security
  , @http_method
  , @path
  , @schema_version
  , @schema
  , @header_settings
  , @query_settings
  , @path_settings
  , @server_env_var
  , @default_server_url
  , @request_content_type
  , @response_filter
  , @read_only_hint
  , @destructive_hint
  , @idempotent_hint
  , @open_world_hint
)
RETURNING *;

-- name: CreateHTTPSecurity :one
INSERT INTO http_security (
    key
  , deployment_id
  , project_id
  , openapiv3_document_id
  , type
  , name
  , in_placement
  , scheme
  , bearer_format
  , env_variables
  , oauth_types
  , oauth_flows
) VALUES (
    @key
  , @deployment_id
  , @project_id
  , @openapiv3_document_id
  , @type
  , @name
  , @in_placement
  , @scheme
  , @bearer_format
  , @env_variables
  , @oauth_types
  , @oauth_flows
)
RETURNING *;

-- name: CreateFunctionsTool :one
INSERT INTO function_tool_definitions (
    deployment_id
  , function_id
  , tool_urn
  , project_id
  , runtime
  , name
  , description
  , input_schema
  , variables
  , auth_input
  , meta
  , read_only_hint
  , destructive_hint
  , idempotent_hint
  , open_world_hint
) VALUES (
    @deployment_id
  , @function_id
  , @tool_urn
  , @project_id
  , @runtime
  , @name
  , @description
  , @input_schema
  , @variables
  , @auth_input
  , @meta
  , @read_only_hint
  , @destructive_hint
  , @idempotent_hint
  , @open_world_hint
)
RETURNING *;

-- name: CreateFunctionsResource :one
INSERT INTO function_resource_definitions (
    deployment_id
  , function_id
  , resource_urn
  , project_id
  , runtime
  , name
  , description
  , uri
  , title
  , mime_type
  , variables
  , meta
) VALUES (
    @deployment_id
  , @function_id
  , @resource_urn
  , @project_id
  , @runtime
  , @name
  , @description
  , @uri
  , @title
  , @mime_type
  , @variables
  , @meta
)
RETURNING *;

-- name: DescribeDeploymentPackages :many
SELECT 
  deployments_packages.id as deployment_package_id
  , packages.name as package_name
  , sqlc.embed(package_versions)
FROM deployments_packages
INNER JOIN packages ON deployments_packages.package_id = packages.id
INNER JOIN package_versions ON deployments_packages.version_id = package_versions.id
WHERE deployments_packages.deployment_id = @deployment_id;

-- name: DangerouslyClearDeploymentTools :execrows
DELETE FROM http_tool_definitions
WHERE
  project_id = @project_id
  AND deployment_id = @deployment_id
  AND openapiv3_document_id = @openapiv3_document_id::uuid;

-- name: DangerouslyClearDeploymentHTTPSecurity :execrows
DELETE FROM http_security
WHERE
  project_id = @project_id
  AND (deployment_id = @deployment_id AND deployment_id IS NOT NULL)
  AND (openapiv3_document_id = @openapiv3_document_id AND openapiv3_document_id IS NOT NULL);

-- name: GetDeploymentFunctions :many
SELECT df.*
FROM deployments_functions df
INNER JOIN deployments d ON df.deployment_id = d.id
WHERE 
  d.project_id = @project_id
  AND df.deployment_id = @deployment_id;

-- name: GetFunctionCredentialsBatch :many
SELECT DISTINCT ON (function_id)
  id,
  function_id,
  encryption_key,
  bearer_format
FROM functions_access
WHERE project_id = @project_id
  AND deployment_id = @deployment_id
  AND function_id = ANY(@function_ids::uuid[])
  AND deleted IS FALSE
ORDER BY function_id, seq DESC;

-- name: GetDeploymentFunctionsWithoutAccess :many
SELECT df.id
FROM deployments_functions df
LEFT JOIN functions_access fm ON df.id = fm.function_id AND fm.deleted IS FALSE
WHERE df.deployment_id = @deployment_id 
  AND df.deployment_id IN (
    SELECT id FROM deployments d WHERE d.project_id = @project_id
  )
  AND fm.function_id IS NULL;

-- name: CreateDeploymentFunctionsAccess :one
INSERT INTO functions_access (
    project_id
  , deployment_id
  , function_id
  , encryption_key
  , bearer_format
) VALUES (
    @project_id
  , @deployment_id
  , @function_id
  , @encryption_key
  , @bearer_format
)
RETURNING id;

-- name: UpsertDeploymentExternalMCP :one
INSERT INTO external_mcp_attachments (deployment_id, registry_id, name, slug, registry_server_specifier)
VALUES (@deployment_id, @registry_id, @name, @slug, @registry_server_specifier)
ON CONFLICT (deployment_id, slug) WHERE deleted IS FALSE
DO UPDATE SET
  registry_id = EXCLUDED.registry_id,
  name = EXCLUDED.name,
  registry_server_specifier = EXCLUDED.registry_server_specifier,
  updated_at = clock_timestamp()
RETURNING id, deployment_id, registry_id, name, slug, registry_server_specifier, created_at, updated_at;

-- name: ListDeploymentExternalMCPs :many
SELECT id, deployment_id, registry_id, name, slug, registry_server_specifier, created_at, updated_at
FROM external_mcp_attachments
WHERE deployment_id = @deployment_id AND deleted IS FALSE
ORDER BY created_at ASC;

-- name: CloneDeploymentExternalMCPs :many
INSERT INTO external_mcp_attachments (deployment_id, registry_id, name, slug, registry_server_specifier)
SELECT
  @clone_deployment_id
  , current.registry_id
  , current.name
  , current.slug
  , current.registry_server_specifier
FROM external_mcp_attachments as current
WHERE current.deployment_id = @original_deployment_id
  AND current.deleted IS FALSE
  AND current.slug <> ALL (@excluded_slugs::text[])
RETURNING id, deployment_id, registry_id, name, slug, registry_server_specifier;

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
  COUNT(DISTINCT doa.id) as asset_count,
  COUNT(DISTINCT htd.id) as tool_count
FROM deployments d
LEFT JOIN latest_statuses ls ON d.id = ls.deployment_id
LEFT JOIN deployments_openapiv3_assets doa ON d.id = doa.deployment_id
LEFT JOIN http_tool_definitions htd ON d.id = htd.deployment_id AND htd.deleted IS FALSE
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
  log.event,
  log.message,
  log.attachment_id,
  log.attachment_type,
  log.created_at
FROM deployment_logs log
WHERE
  log.deployment_id = @deployment_id AND log.project_id = @project_id
  AND log.id >= CASE 
    WHEN sqlc.narg(cursor)::uuid IS NOT NULL THEN sqlc.narg(cursor)::uuid
    ELSE (
      SELECT dl.id
      FROM deployment_logs dl
      WHERE dl.deployment_id = @deployment_id AND dl.project_id = @project_id
      ORDER BY dl.id ASC LIMIT 1
    )
  END
ORDER BY log.id ASC
LIMIT 51;

-- name: GetDeploymentWithAssets :many
WITH latest_status as (
    SELECT deployment_id, status
    FROM deployment_statuses
    WHERE deployment_id = @id
    ORDER BY seq DESC
    LIMIT 1
),
tool_counts as (
    SELECT 
        deployment_id,
        COUNT(DISTINCT id) as tool_count
    FROM http_tool_definitions 
    WHERE deployment_id = @id AND deleted IS FALSE
    GROUP BY deployment_id
)
SELECT
  sqlc.embed(deployments),
  coalesce(latest_status.status, 'unknown') as status,
  deployments_openapiv3_assets.id as deployments_openapiv3_asset_id,
  deployments_openapiv3_assets.asset_id as deployments_openapiv3_asset_store_id,
  deployments_openapiv3_assets.name as deployments_openapiv3_asset_name,
  deployments_openapiv3_assets.slug as deployments_openapiv3_asset_slug,
  deployments_packages.package_id as deployment_package_id,
  packages.name as package_name,
  package_versions.major as package_version_major,
  package_versions.minor as package_version_minor,
  package_versions.patch as package_version_patch,
  package_versions.prerelease as package_version_prerelease,
  package_versions.build as package_version_build,
  COALESCE(tool_counts.tool_count, 0) as tool_count
FROM deployments
LEFT JOIN deployments_openapiv3_assets ON deployments.id = deployments_openapiv3_assets.deployment_id
LEFT JOIN deployments_packages ON deployments.id = deployments_packages.deployment_id
LEFT JOIN latest_status ON deployments.id = latest_status.deployment_id
LEFT JOIN packages ON deployments_packages.package_id = packages.id
LEFT JOIN package_versions ON deployments_packages.version_id = package_versions.id
LEFT JOIN tool_counts ON deployments.id = tool_counts.deployment_id
WHERE deployments.id = @id AND deployments.project_id = @project_id;

-- name: GetLatestDeploymentID :one
SELECT id
FROM deployments
WHERE project_id = @project_id
ORDER BY id DESC
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
) VALUES (
    @project_id
  , @deployment_id
  , @openapiv3_document_id
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

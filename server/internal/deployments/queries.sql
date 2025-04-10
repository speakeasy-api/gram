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
SELECT 
  d.id,
  d.user_id,
  d.created_at,
  COUNT(doa.id) as asset_count
FROM deployments d
LEFT JOIN deployments_openapiv3_assets doa ON d.id = doa.deployment_id
WHERE
  d.project_id = @project_id
  AND d.id <= CASE 
    WHEN sqlc.narg(cursor)::uuid IS NOT NULL THEN sqlc.narg(cursor)::uuid
    ELSE (SELECT id FROM deployments WHERE project_id = @project_id ORDER BY id DESC LIMIT 1)
  END
GROUP BY d.id
ORDER BY d.id DESC
LIMIT 51;

-- name: GetDeploymentWithAssets :many
WITH latest_status as (
    SELECT deployment_id, status
    FROM deployment_statuses
    WHERE deployment_id = @id
    ORDER BY seq DESC
    LIMIT 1
)
SELECT sqlc.embed(deployments), sqlc.embed(deployments_openapiv3_assets), coalesce(latest_status.status, 'unknown') as status
FROM deployments
LEFT JOIN deployments_openapiv3_assets ON deployments.id = deployments_openapiv3_assets.deployment_id
LEFT JOIN latest_status ON deployments.id = latest_status.deployment_id
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
    WHERE deployment_id = @id
    ORDER BY seq DESC
    LIMIT 1
)
SELECT sqlc.embed(deployments), coalesce(latest_status.status, 'unknown') as status
FROM deployments
LEFT JOIN latest_status ON deployments.id = latest_status.deployment_id
WHERE idempotency_key = @idempotency_key
 AND project_id = @project_id;

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
  , external_id
  , external_url
) VALUES (
  @idempotency_key
  , @user_id
  , @organization_id
  , @project_id
  , @github_repo
  , @github_pr
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
  , current.external_id
  , current.external_url
FROM deployments as current
WHERE current.id = @id AND current.project_id = @project_id
RETURNING id;

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
RETURNING id;

-- name: TransitionDeployment :one
WITH status AS (
  INSERT INTO deployment_statuses (deployment_id , status)
  VALUES (@deployment_id, @status)
  RETURNING id, status
), 
log AS (
  INSERT INTO deployment_logs (deployment_id, project_id, event, message)
  VALUES (@deployment_id, @project_id, @event, @message)
  RETURNING id
)
SELECT status.id as status_id, status.status as status, log.id as log_id
FROM status, log;

-- name: LogDeploymentEvent :exec
INSERT INTO deployment_logs (deployment_id, project_id, event, message)
VALUES (@deployment_id, @project_id, @event, @message);

-- name: AddDeploymentOpenAPIv3Asset :one
INSERT INTO deployments_openapiv3_assets (
  deployment_id
  , asset_id
  , name
  , slug
) VALUES (
  @deployment_id,
  @asset_id,
  @name,
  @slug
)
ON CONFLICT (deployment_id, slug) DO NOTHING
RETURNING id, asset_id, name, slug;

-- name: CreateOpenAPIv3ToolDefinition :one
INSERT INTO http_tool_definitions (
    project_id
  , deployment_id
  , openapiv3_document_id
  , name
  , openapiv3_operation
  , summary
  , description
  , tags
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
) VALUES (
    @project_id
  , @deployment_id
  , @openapiv3_document_id
  , @name
  , @openapiv3_operation
  , @summary
  , @description
  , @tags
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
)
RETURNING *;

-- name: CreateHTTPSecurity :one
INSERT INTO http_security (
    key
  , deployment_id
  , type
  , name
  , in_placement
  , scheme
  , bearer_format
  , env_variables
) VALUES (
    @key
  , @deployment_id
  , @type
  , @name
  , @in_placement
  , @scheme
  , @bearer_format
  , @env_variables
)
RETURNING *;

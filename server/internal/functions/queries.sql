-- name: GetFlyAppAccess :one
SELECT
    fa.fly_org_id
  , fa.fly_org_slug
  , fa.app_name
  , fa.app_url
  , fa.runner_version
  , access.id AS access_id
  , access.encryption_key
  , access.bearer_format
FROM fly_apps fa
LEFT JOIN functions_access access
  ON access.id = fa.access_id
  AND access.deleted IS FALSE
WHERE
  fa.project_id = @project_id
  AND fa.deployment_id = @deployment_id
  AND fa.function_id = @function_id
  AND fa.status = 'ready'
  AND fa.reaped_at IS NULL
  AND fa.access_id = @access_id
ORDER BY fa.seq DESC NULLS LAST
LIMIT 1;

-- name: GetFunctionsRunnerVersion :one
WITH project_preference AS (
  SELECT p.functions_runner_version as v
  FROM projects p
  WHERE p.id = @project_id
    AND deleted IS FALSE
),
function_preference AS (
  SELECT df.runner_version as v
  FROM deployments_functions df
  INNER JOIN deployments d ON df.deployment_id = d.id
  WHERE
    d.project_id = @project_id
    AND df.id = @function_id
    AND df.deployment_id = @deployment_id
)
SELECT COALESCE(
    (SELECT v FROM function_preference),
    (SELECT v FROM project_preference),
    ''
)::text as runner_version;

-- name: InitFlyApp :one
INSERT INTO fly_apps (
    project_id
  , deployment_id
  , function_id
  , access_id
  , fly_org_id
  , fly_org_slug
  , app_name
  , app_url
  , runner_version
  , primary_region
  , status
) VALUES (
    @project_id
  , @deployment_id
  , @function_id
  , @access_id
  , @fly_org_id
  , @fly_org_slug
  , @app_name
  , @app_url
  , @runner_version
  , @primary_region
  , 'pending'
) RETURNING id;

-- name: FinalizeFlyApp :one
UPDATE fly_apps SET
  status = @status,
  reaped_at = @reaped_at,
  updated_at = clock_timestamp()
WHERE
  id = @id
  AND project_id = @project_id
  AND deployment_id = @deployment_id
  AND function_id = @function_id
RETURNING id;

-- name: GetFlyAppsToReap :many
WITH ranked_deployments AS (
  -- This CTE ranks deployments within each project by their creation time.
  -- Using d.created_at (deployment timestamp) ensures all functions within
  -- a deployment share the same rank, preventing partial reaping where some
  -- functions are deleted but others remain. DENSE_RANK ensures consecutive
  -- ranks even if multiple deployments have the same timestamp.
  SELECT DISTINCT
      fa.project_id
    , fa.deployment_id
    , d.created_at
    , DENSE_RANK() OVER (
        PARTITION BY fa.project_id
        ORDER BY d.created_at DESC
      ) as deployment_rank
  FROM fly_apps fa
  INNER JOIN deployments d ON d.id = fa.deployment_id
  WHERE
    fa.status = 'ready'
    AND (@project_id::uuid IS NULL OR fa.project_id = @project_id)
    AND fa.reaped_at IS NULL
)
SELECT
    fa.id
  , fa.project_id
  , fa.deployment_id
  , fa.function_id
  , fa.fly_org_slug
  , fa.app_name
  , fa.created_at
FROM fly_apps fa
INNER JOIN ranked_deployments rd
  ON fa.project_id = rd.project_id
  AND fa.deployment_id = rd.deployment_id
WHERE
  rd.deployment_rank > @keep_count
  AND fa.status = 'ready'
  AND fa.reaped_at IS NULL
ORDER BY fa.created_at ASC
LIMIT @batch_size;

-- name: MarkFlyAppReaped :exec
UPDATE fly_apps SET
  reap_error = @reap_error,
  reaped_at = @reaped_at,
  updated_at = clock_timestamp()
WHERE id = @id;

-- name: GetFlyAppNameForFunction :one
SELECT
    id
  , fly_org_slug
  , app_name
FROM fly_apps
WHERE
  project_id = @project_id
  AND deployment_id = @deployment_id
  AND function_id = @function_id
  AND status = 'ready'
  AND reaped_at IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: GetFunctionAssetURL :one
SELECT a.url
FROM deployments_functions df
INNER JOIN assets a ON df.asset_id = a.id
WHERE
  a.project_id = @project_id
  AND df.deployment_id = @deployment_id
  AND df.id = @function_id
  AND a.id = @asset_id
  AND a.deleted IS FALSE;

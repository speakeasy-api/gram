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
    (SELECT v FROM project_preference)
)::TEXT as runner_version;

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
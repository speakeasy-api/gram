-- name: GetDeployment :one
SELECT *
FROM deployments
WHERE id = @id;

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
SELECT sqlc.embed(deployments), sqlc.embed(deployments_openapiv3_assets)
FROM deployments
LEFT JOIN deployments_openapiv3_assets ON deployments.id = deployments_openapiv3_assets.deployment_id
WHERE deployments.id = @id AND deployments.project_id = @project_id;

-- name: GetDeploymentByIdempotencyKey :one
SELECT *
FROM deployments
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
RETURNING id, asset_id, name, slug;
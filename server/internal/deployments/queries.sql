-- name: GetDeployment :one
SELECT 
    id
  , user_id
  , organization_id
  , project_id
  , manifest_version
  , manifest_url
  , github_repo
  , github_pr
  , external_id
  , external_url
  , created_at
  , updated_at
FROM deployments
WHERE id = @id;

-- name: CreateDeployment :one
INSERT INTO deployments (
    user_id
  , manifest_version
  , manifest_url
  , organization_id
  , project_id
  , github_repo
  , github_pr
  , external_id
  , external_url
) VALUES (
    @user_id
  , @manifest_version
  , @manifest_url
  , @organization_id
  , @project_id
  , @github_repo
  , @github_pr
  , @external_id
  , @external_url
)
RETURNING 
    id
  , user_id
  , organization_id
  , project_id
  , manifest_version
  , manifest_url
  , github_repo
  , github_pr
  , external_id
  , external_url
  , created_at
  , updated_at;

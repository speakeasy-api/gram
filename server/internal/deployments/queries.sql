-- name: GetDeployment :one
SELECT 
    id
  , user_id
  , organization_id
  , workspace_id
  , external_id
  , external_url
  , created_at
  , updated_at
FROM deployments
WHERE id = @id;

-- name: CreateDeployment :one
INSERT INTO deployments (
    user_id
  , organization_id
  , workspace_id
  , external_id
  , external_url
) VALUES (
    @user_id
  , @organization_id
  , @workspace_id
  , @external_id
  , @external_url
)
RETURNING 
    id
  , user_id
  , organization_id
  , workspace_id
  , external_id
  , external_url
  , created_at
  , updated_at;

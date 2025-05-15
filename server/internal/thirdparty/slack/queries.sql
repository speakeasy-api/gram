-- name: CreateSlackAppConnection :one
INSERT INTO slack_app_connections (
    organization_id
  , project_id
  , access_token
  , slack_team_name
  , slack_team_id
  , default_toolset_slug
) VALUES (
    @organization_id
  , @project_id
  , @access_token
  , @slack_team_name
  , @slack_team_id
  , @default_toolset_slug
)
RETURNING *;

-- name: UpdateSlackAppConnection :one
UPDATE slack_app_connections
SET
    default_toolset_slug = @default_toolset_slug,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
RETURNING *;

-- name: GetSlackAppConnection :one
SELECT *
FROM slack_app_connections
WHERE organization_id = @organization_id
  AND project_id = @project_id;

-- name: DeleteSlackAppConnection :exec
DELETE FROM slack_app_connections
WHERE organization_id = @organization_id
  AND project_id = @project_id;
-- name: CreateSlackApp :one
INSERT INTO slack_apps (
    organization_id
  , project_id
  , name
  , system_prompt
  , icon_asset_id
  , status
) VALUES (
    @organization_id
  , @project_id
  , @name
  , sqlc.narg('system_prompt')
  , sqlc.narg('icon_asset_id')
  , 'unconfigured'
)
RETURNING *;

-- name: GetSlackApp :one
SELECT *
FROM slack_apps
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: GetSlackAppByTeamID :one
SELECT *
FROM slack_apps
WHERE slack_team_id = @slack_team_id
  AND deleted IS FALSE;

-- name: ListSlackApps :many
SELECT *
FROM slack_apps
WHERE project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateSlackApp :one
UPDATE slack_apps
SET
    name = COALESCE(sqlc.narg('name'), name),
    system_prompt = COALESCE(sqlc.narg('system_prompt'), system_prompt),
    icon_asset_id = COALESCE(sqlc.narg('icon_asset_id'), icon_asset_id),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: ConfigureSlackApp :one
UPDATE slack_apps
SET
    slack_client_id = @slack_client_id,
    slack_client_secret = @slack_client_secret,
    slack_signing_secret = @slack_signing_secret,
    status = 'active',
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteSlackApp :exec
UPDATE slack_apps
SET
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: AddSlackAppToolset :one
INSERT INTO slack_app_toolsets (
    slack_app_id
  , toolset_id
) VALUES (
    @slack_app_id
  , @toolset_id
)
RETURNING *;

-- name: RemoveSlackAppToolset :exec
DELETE FROM slack_app_toolsets
WHERE slack_app_id = @slack_app_id
  AND toolset_id = @toolset_id;

-- name: InstallSlackApp :one
UPDATE slack_apps
SET
    slack_bot_token  = @slack_bot_token,
    slack_team_id    = @slack_team_id,
    slack_team_name  = @slack_team_name,
    slack_bot_user_id = @slack_bot_user_id,
    updated_at       = clock_timestamp()
WHERE id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: GetSlackAppByID :one
SELECT *
FROM slack_apps
WHERE id = @id
  AND deleted IS FALSE;

-- name: ListSlackAppToolsets :many
SELECT sat.*
FROM slack_app_toolsets sat
JOIN slack_apps sa ON sa.id = sat.slack_app_id
WHERE sat.slack_app_id = @slack_app_id
  AND sa.project_id = @project_id
  AND sa.deleted IS FALSE
ORDER BY sat.created_at ASC;

-- name: ListSlackAppToolsetNames :many
SELECT t.name, t.slug
FROM slack_app_toolsets sat
JOIN toolsets t ON t.id = sat.toolset_id
JOIN slack_apps sa ON sa.id = sat.slack_app_id
WHERE sat.slack_app_id = @slack_app_id
  AND sa.deleted IS FALSE
  AND t.deleted IS FALSE
ORDER BY t.name ASC;

-- name: GetSlackRegistration :one
SELECT *
FROM slack_registrations
WHERE slack_app_id = @slack_app_id
  AND slack_account_id = @slack_account_id;

-- name: GetSlackRegistrationWithUser :one
SELECT sr.*, u.display_name AS user_display_name, u.email AS user_email
FROM slack_registrations sr
JOIN users u ON u.id = sr.user_id::text
WHERE sr.slack_app_id = @slack_app_id
  AND sr.slack_account_id = @slack_account_id;

-- name: CreateSlackRegistration :one
INSERT INTO slack_registrations (slack_app_id, slack_account_id, user_id)
VALUES (@slack_app_id, @slack_account_id, @user_id)
ON CONFLICT (slack_app_id, slack_account_id) DO UPDATE SET user_id = @user_id, updated_at = clock_timestamp()
RETURNING *;

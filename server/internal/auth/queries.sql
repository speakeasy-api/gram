-- name: SetUserAdminFixture :exec
UPDATE users
SET admin = @admin
WHERE id = @user_id;

-- name: ListProjectsByOrganization :many
SELECT *
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: PokeProjectByID :one
SELECT id
FROM projects
WHERE
  organization_id = @organization_id
  AND id = @project_id
  AND deleted IS FALSE;
-- name: AcquirePlatformMCPWorkspaceLock :exec
-- Serialize Platform MCP workspace creation per user, so two concurrent
-- OAuth submits cannot each create a first organization with a trial.
SELECT pg_advisory_lock(hashtextextended('platform-mcp-workspace:' || @user_id::text, 0));

-- name: ReleasePlatformMCPWorkspaceLock :one
SELECT pg_advisory_unlock(hashtextextended('platform-mcp-workspace:' || @user_id::text, 0)) AS unlocked;

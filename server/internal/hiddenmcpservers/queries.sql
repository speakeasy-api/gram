-- name: ListHiddenMCPServerIDs :many
SELECT mcp_server_id
FROM user_hidden_mcp_servers
WHERE organization_id = @organization_id
  AND user_id = @user_id;

-- Hidden servers that still exist, for showing the preference back.
-- name: ListHiddenMCPServers :many
SELECT
  h.mcp_server_id,
  h.project_id,
  h.created_at,
  ms.slug,
  ms.name,
  p.slug AS project_slug
FROM user_hidden_mcp_servers AS h
JOIN mcp_servers AS ms ON ms.id = h.mcp_server_id AND ms.project_id = h.project_id
JOIN projects AS p ON p.id = h.project_id
WHERE h.organization_id = @organization_id
  AND h.user_id = @user_id
  AND p.organization_id = @organization_id
  AND ms.deleted IS FALSE
  AND p.deleted IS FALSE
ORDER BY h.created_at, h.mcp_server_id;

-- Hides a live server in one of the organization's projects. No row means no
-- such server in the organization.
-- name: HideMCPServer :one
INSERT INTO user_hidden_mcp_servers (organization_id, user_id, mcp_server_id, project_id)
SELECT p.organization_id, @user_id, ms.id, ms.project_id
FROM mcp_servers AS ms
JOIN projects AS p ON p.id = ms.project_id
WHERE ms.id = @mcp_server_id
  AND p.organization_id = @organization_id
  AND ms.deleted IS FALSE
  AND p.deleted IS FALSE
ON CONFLICT (organization_id, user_id, mcp_server_id) DO UPDATE
SET updated_at = clock_timestamp()
RETURNING *;

-- name: UnhideMCPServer :execrows
DELETE FROM user_hidden_mcp_servers
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND mcp_server_id = @mcp_server_id;

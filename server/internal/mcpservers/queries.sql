-- name: CreateMCPServer :one
INSERT INTO mcp_servers (
    id,
    project_id,
    name,
    slug,
    environment_id,
    user_session_issuer_id,
    remote_mcp_server_id,
    tunnelled_mcp_server_id,
    toolset_id,
    tool_variations_group_id,
    visibility
)
VALUES (
    @id,
    @project_id,
    @name,
    @slug,
    @environment_id,
    @user_session_issuer_id,
    @remote_mcp_server_id,
    @tunnelled_mcp_server_id,
    @toolset_id,
    @tool_variations_group_id,
    @visibility
)
RETURNING *;

-- name: GetMCPServerByIDAndProjectID :one
SELECT *
FROM mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetMCPServerByIDAndOrganizationID :one
-- Fetch an MCP server by id scoped to an organization via its project's
-- organization_id. For organization-administrator flows that span projects but
-- must stay within the caller's org (e.g. remote session client detach).
SELECT m.*
FROM mcp_servers AS m
JOIN projects AS p ON p.id = m.project_id
WHERE m.id = @id
  AND p.organization_id = @organization_id
  AND m.deleted IS FALSE;

-- name: GetMCPServerBySlug :one
SELECT *
FROM mcp_servers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListMCPServersByProjectID :many
SELECT *
FROM mcp_servers
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('remote_mcp_server_id')::uuid IS NULL OR remote_mcp_server_id = sqlc.narg('remote_mcp_server_id')::uuid)
  AND (sqlc.narg('tunnelled_mcp_server_id')::uuid IS NULL OR tunnelled_mcp_server_id = sqlc.narg('tunnelled_mcp_server_id')::uuid)
  AND (sqlc.narg('toolset_id')::uuid IS NULL OR toolset_id = sqlc.narg('toolset_id')::uuid)
ORDER BY created_at DESC;

-- name: UpdateMCPServer :one
UPDATE mcp_servers
SET
    name = @name,
    slug = @slug,
    environment_id = @environment_id,
    user_session_issuer_id = @user_session_issuer_id,
    remote_mcp_server_id = @remote_mcp_server_id,
    tunnelled_mcp_server_id = @tunnelled_mcp_server_id,
    toolset_id = @toolset_id,
    tool_variations_group_id = @tool_variations_group_id,
    visibility = @visibility,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteMCPServer :one
UPDATE mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

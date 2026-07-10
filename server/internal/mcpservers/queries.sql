-- name: CreateMCPServer :one
INSERT INTO mcp_servers (
    id,
    project_id,
    name,
    slug,
    environment_id,
    user_session_issuer_id,
    remote_mcp_server_id,
    tunneled_mcp_server_id,
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
    @tunneled_mcp_server_id,
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
  AND (sqlc.narg('tunneled_mcp_server_id')::uuid IS NULL OR tunneled_mcp_server_id = sqlc.narg('tunneled_mcp_server_id')::uuid)
  AND (sqlc.narg('toolset_id')::uuid IS NULL OR toolset_id = sqlc.narg('toolset_id')::uuid)
ORDER BY created_at DESC;

-- name: ListMCPServersForTelemetryByProjectID :many
-- Includes soft-deleted servers so tool-usage telemetry can classify historical
-- rows whose backing MCP server has since been deleted (or recreated). The
-- backend source ids (remote_mcp_server_id / tunneled_mcp_server_id) recorded on
-- telemetry rows outlive the mcp_servers row, so matching against deleted rows
-- keeps a call's target_type stable instead of falling through to
-- shadow_mcp_server. Live servers are ordered first so a source id shared by a
-- live and a deleted server resolves to the live one.
SELECT id, name, slug, remote_mcp_server_id, tunneled_mcp_server_id
FROM mcp_servers
WHERE project_id = @project_id
ORDER BY deleted ASC, created_at DESC;

-- name: UpdateMCPServer :one
UPDATE mcp_servers
SET
    name = @name,
    slug = @slug,
    environment_id = @environment_id,
    -- The issuer is attached at create time for the server's lifetime; updates
    -- can never change or clear it (NULL here always preserves the stored value).
    user_session_issuer_id = COALESCE(sqlc.narg('user_session_issuer_id'), user_session_issuer_id),
    remote_mcp_server_id = @remote_mcp_server_id,
    tunneled_mcp_server_id = @tunneled_mcp_server_id,
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

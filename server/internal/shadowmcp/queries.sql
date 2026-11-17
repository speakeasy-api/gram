-- name: ListShadowMCPOfficialServerChoices :many
-- An administrator's choices about official servers in one project. The
-- chosen server comes back only while it is live and enabled, so a choice
-- whose server was removed falls back to the server found by URL.
SELECT
    c.server_url,
    c.allow_direct_access,
    m.id AS chosen_mcp_server_id,
    m.name AS chosen_mcp_server_name,
    m.slug AS chosen_mcp_server_slug
FROM shadow_mcp_official_servers AS c
LEFT JOIN mcp_servers AS m
    ON m.id = c.mcp_server_id
    AND m.project_id = c.project_id
    AND m.deleted IS FALSE
    AND m.visibility <> 'disabled'
WHERE c.project_id = @project_id
    AND c.deleted IS FALSE;

-- name: GetShadowMCPOfficialServerChoice :one
-- One choice, read the same way as ListShadowMCPOfficialServerChoices.
SELECT
    c.server_url,
    c.allow_direct_access,
    m.id AS chosen_mcp_server_id,
    m.name AS chosen_mcp_server_name,
    m.slug AS chosen_mcp_server_slug
FROM shadow_mcp_official_servers AS c
LEFT JOIN mcp_servers AS m
    ON m.id = c.mcp_server_id
    AND m.project_id = c.project_id
    AND m.deleted IS FALSE
    AND m.visibility <> 'disabled'
WHERE c.project_id = @project_id
    AND c.server_url = @server_url
    AND c.deleted IS FALSE;

-- name: LockShadowMCPOfficialServerChoice :one
-- The live choice row for one URL, locked for the update that replaces it.
SELECT *
FROM shadow_mcp_official_servers
WHERE project_id = @project_id
    AND server_url = @server_url
    AND deleted IS FALSE
FOR UPDATE;

-- name: UpsertShadowMCPOfficialServerChoice :one
INSERT INTO shadow_mcp_official_servers (
    project_id,
    server_url,
    mcp_server_id,
    allow_direct_access,
    created_by_user_id,
    updated_by_user_id
) VALUES (
    @project_id,
    @server_url,
    sqlc.narg(mcp_server_id),
    @allow_direct_access,
    sqlc.narg(user_id),
    sqlc.narg(user_id)
)
ON CONFLICT (project_id, server_url) WHERE deleted IS FALSE DO UPDATE SET
    mcp_server_id = EXCLUDED.mcp_server_id,
    allow_direct_access = EXCLUDED.allow_direct_access,
    updated_by_user_id = EXCLUDED.updated_by_user_id,
    updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteShadowMCPOfficialServerChoice :execrows
UPDATE shadow_mcp_official_servers
SET deleted_at = clock_timestamp(),
    updated_by_user_id = sqlc.narg(user_id),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
    AND server_url = @server_url
    AND deleted IS FALSE;

-- name: GetLiveMCPServerForShadowMCPChoice :one
-- The server an administrator chooses must be live in the same project.
SELECT id, name, slug, visibility
FROM mcp_servers
WHERE id = @id
    AND project_id = @project_id
    AND deleted IS FALSE;

-- name: ListDeniedShadowMCPServerURLs :many
-- The server URLs whose standing decision in a project is a denial. With no
-- blocking Shadow MCP policy, a project is in the built-in blocklist mode:
-- every server connects except these. A superseded request carries no
-- standing decision.
SELECT r.target_key
FROM mcp_approval_requests r
WHERE r.project_id = @project_id
  AND r.target_kind = 'server_url'
  AND r.deleted IS FALSE
  AND r.status <> 'superseded'
  AND (
    SELECT d.decision
    FROM mcp_approval_decisions d
    WHERE d.mcp_approval_request_id = r.id
      AND d.project_id = r.project_id
      AND d.deleted IS FALSE
    ORDER BY d.decided_at DESC, d.id DESC
    LIMIT 1
  ) = 'denied';

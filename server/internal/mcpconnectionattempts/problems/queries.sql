-- name: ListServerUpstreams :many
-- Remote URL and issuer, each when set, of the live MCP servers among the ids in the organization.
SELECT
    m.id,
    rms.url,
    rsi.issuer
FROM mcp_servers AS m
JOIN projects AS p
    ON p.id = m.project_id
    AND p.organization_id = @organization_id
    AND p.deleted IS FALSE
LEFT JOIN remote_mcp_servers AS rms
    ON rms.id = m.remote_mcp_server_id
    AND rms.project_id = m.project_id
    AND rms.deleted IS FALSE
LEFT JOIN remote_session_issuers AS rsi
    ON rsi.id = m.remote_session_issuer_id
    AND rsi.deleted IS FALSE
    AND (
        rsi.project_id = m.project_id
        OR (rsi.project_id IS NULL AND (rsi.organization_id = @organization_id OR rsi.organization_id IS NULL))
    )
WHERE m.id = ANY(@server_ids::uuid[])
    AND m.deleted IS FALSE
    AND (rms.id IS NOT NULL OR rsi.id IS NOT NULL);

-- name: SoftDeleteMCPServerFixture :execrows
-- Test fixture: soft-deletes an MCP server in a project.
UPDATE mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id
    AND project_id = @project_id;

-- name: CreateTunneledMCPServerFixture :one
-- Test fixture: an MCP server backed by a tunnel in a project.
WITH tunnel AS (
    INSERT INTO tunneled_mcp_servers (project_id, name, key_hash, key_prefix)
    VALUES (@project_id, @name::text, @name::text, @name::text)
    RETURNING id, project_id
)
INSERT INTO mcp_servers (project_id, slug, tunneled_mcp_server_id, visibility)
SELECT tunnel.project_id, @name::text, tunnel.id, 'private'
FROM tunnel
RETURNING id;

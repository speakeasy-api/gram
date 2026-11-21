-- name: ListServerUpstreams :many
-- URL and, when set, issuer of the live remote-backed MCP servers among the ids in the organization.
SELECT
    m.id,
    rms.url,
    rsi.issuer
FROM mcp_servers AS m
JOIN projects AS p
    ON p.id = m.project_id
    AND p.organization_id = @organization_id
    AND p.deleted IS FALSE
JOIN remote_mcp_servers AS rms
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
    AND m.deleted IS FALSE;

-- name: SoftDeleteMCPServerFixture :execrows
-- Test fixture: soft-deletes an MCP server in a project.
UPDATE mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id
    AND project_id = @project_id;

-- name: ListServerUpstreams :many
-- The configured upstream of each live remote-backed MCP server among the ids
-- whose project belongs to the organization: its URL, and the issuer it signs
-- in to when one is set. Ids from other organizations return nothing.
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

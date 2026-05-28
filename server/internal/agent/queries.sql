-- name: GetAssignedPluginsWithServers :many
-- Resolves plugins assigned to any of the given principal URNs within an org,
-- joining server + toolset rows so the handler can construct MCP URLs in one
-- pass. Returns one row per (plugin, server). Callers regroup by plugin_id.
SELECT
  p.id AS plugin_id,
  p.name AS plugin_name,
  p.slug AS plugin_slug,
  p.description AS plugin_description,
  p.updated_at AS plugin_updated_at,
  ps.id AS server_id,
  ps.display_name AS server_display_name,
  ps.policy AS server_policy,
  ps.sort_order AS server_sort_order,
  ps.updated_at AS server_updated_at,
  t.mcp_slug AS toolset_mcp_slug,
  t.mcp_is_public AS toolset_mcp_is_public
FROM plugin_assignments pa
JOIN plugins p
  ON p.id = pa.plugin_id
  AND p.deleted IS FALSE
JOIN plugin_servers ps
  ON ps.plugin_id = p.id
  AND ps.deleted IS FALSE
JOIN toolsets t
  ON t.id = ps.toolset_id
  AND t.deleted IS FALSE
  AND t.mcp_enabled IS TRUE
WHERE pa.organization_id = @organization_id
  AND pa.principal_urn = ANY(@principal_urns::text[])
ORDER BY p.slug, ps.sort_order;

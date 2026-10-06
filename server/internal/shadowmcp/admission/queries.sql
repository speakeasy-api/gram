-- name: IsGatewayDistributedToPrincipal :one
SELECT EXISTS (
  SELECT 1 FROM plugin_servers ps
  JOIN plugins p ON p.id = ps.plugin_id AND p.organization_id = sqlc.arg(organization_id) AND p.project_id = sqlc.arg(project_id) AND p.deleted IS FALSE
  JOIN plugin_assignments pa ON pa.plugin_id = p.id AND pa.organization_id = p.organization_id AND pa.principal_urn = sqlc.arg(principal_urn)
  WHERE ps.meta_mcp_server_id = sqlc.arg(gateway_id) AND ps.deleted IS FALSE
);

-- name: GetPluginGatewayNetworkAccessMode :one
SELECT g.network_access_mode
FROM meta_mcp_servers g
JOIN plugins p ON p.organization_id = g.organization_id AND p.project_id = g.project_id AND p.deleted IS FALSE
WHERE p.id = sqlc.arg(plugin_id) AND g.id = sqlc.arg(gateway_id)
  AND g.organization_id = sqlc.arg(organization_id) AND g.project_id = sqlc.arg(project_id) AND g.deleted IS FALSE;

-- name: ListPluginGatewayNetworkAccessModes :many
SELECT g.network_access_mode
FROM plugin_servers ps
JOIN plugins p ON p.id = ps.plugin_id AND p.organization_id = sqlc.arg(organization_id) AND p.project_id = sqlc.arg(project_id) AND p.deleted IS FALSE
JOIN meta_mcp_servers g ON g.id = ps.meta_mcp_server_id AND g.organization_id = p.organization_id AND g.project_id = p.project_id AND g.deleted IS FALSE
WHERE ps.plugin_id = sqlc.arg(plugin_id) AND ps.deleted IS FALSE AND g.visibility <> 'disabled';

-- name: ListPluginAudience :many
SELECT assignment.principal_urn
FROM plugin_assignments AS assignment
JOIN plugins AS plugin ON plugin.id = assignment.plugin_id
  AND plugin.organization_id = assignment.organization_id AND plugin.deleted IS FALSE
WHERE assignment.plugin_id = sqlc.arg(plugin_id)
  AND assignment.organization_id = sqlc.arg(organization_id) AND plugin.project_id = sqlc.arg(project_id)
ORDER BY assignment.principal_urn;

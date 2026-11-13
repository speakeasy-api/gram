-- name: ListProjectResourceIndexEnvironments :many
SELECT id, name, slug FROM environments WHERE project_id = @project_id AND deleted IS FALSE ORDER BY name, id;
-- name: ListProjectResourceIndexGateways :many
SELECT id, name FROM meta_mcp_servers WHERE organization_id = @organization_id AND project_id = @project_id AND deleted IS FALSE ORDER BY name, id;
-- name: ListProjectResourceIndexPlugins :many
SELECT id, name, slug FROM plugins WHERE organization_id = @organization_id AND project_id = @project_id AND deleted IS FALSE ORDER BY name, id;
-- name: ListProjectResourceIndexCustomRules :many
SELECT id, title, rule_id, severity FROM risk_custom_detection_rules WHERE organization_id = @organization_id AND project_id = @project_id AND deleted IS FALSE ORDER BY title, id;
-- name: ListProjectResourceIndexAssistants :many
SELECT id, name FROM assistants WHERE project_id = @project_id AND deleted IS FALSE ORDER BY name, id;
-- name: ListProjectResourceIndexMCPServers :many
WITH candidates AS (
  SELECT ms.id, ms.name, ms.slug, ms.id AS route_id FROM mcp_servers ms
  WHERE ms.project_id = @project_id AND ms.deleted IS FALSE AND ms.toolset_id IS NULL AND ms.builtin_backend IS NULL
    AND num_nonnulls(ms.remote_mcp_server_id, ms.tunneled_mcp_server_id, ms.unproxied_mcp_server_id) > 0
  UNION ALL
  SELECT t.id, t.name, t.slug, host.id AS route_id FROM toolsets t
  JOIN LATERAL (
    SELECT ms.id FROM mcp_servers ms
    WHERE ms.project_id = t.project_id AND ms.toolset_id = t.id AND ms.deleted IS FALSE AND ms.builtin_backend IS NULL
      AND (ms.id = t.id OR EXISTS (SELECT 1 FROM mcp_endpoints e WHERE e.project_id = ms.project_id AND e.mcp_server_id = ms.id AND e.deleted IS FALSE))
    ORDER BY (ms.id = t.id) DESC, ms.created_at, ms.id LIMIT 1
  ) host ON TRUE
  WHERE t.project_id = @project_id AND t.deleted IS FALSE
)
SELECT id, name, slug, route_id FROM candidates ORDER BY name, id;
-- name: ListProjectResourceIndexSkills :many
SELECT id, display_name, name FROM skills WHERE project_id = @project_id AND archived_at IS NULL ORDER BY display_name, id;

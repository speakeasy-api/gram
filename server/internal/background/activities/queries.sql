-- name: GetPlatformUsageMetrics :many
-- Get comprehensive platform usage metrics per organization
WITH latest_deployments AS (
  SELECT DISTINCT ON (project_id) project_id, id as deployment_id
  FROM deployments 
  ORDER BY project_id, created_at DESC
),
toolset_metrics AS (
  SELECT 
    p.organization_id,
    COUNT(CASE WHEN t.mcp_is_public = true AND t.mcp_slug IS NOT NULL THEN 1 END) as public_mcp_servers,
    COUNT(CASE WHEN t.mcp_is_public = false AND t.mcp_slug IS NOT NULL THEN 1 END) as private_mcp_servers,
    COUNT(CASE WHEN t.mcp_enabled = true THEN 1 END) as total_enabled_servers,
    COUNT(t.id) as total_toolsets
  FROM projects p
  LEFT JOIN toolsets t ON p.id = t.project_id AND t.deleted = false
  GROUP BY p.organization_id
),
tool_metrics AS (
  SELECT 
    p.organization_id,
    COUNT(DISTINCT htd.id) as total_tools
  FROM projects p
  LEFT JOIN latest_deployments ld ON p.id = ld.project_id
  LEFT JOIN http_tool_definitions htd ON ld.deployment_id = htd.deployment_id AND htd.deleted = false
  GROUP BY p.organization_id
)
SELECT 
  COALESCE(tm.organization_id, tlm.organization_id) as organization_id,
  COALESCE(tm.public_mcp_servers, 0) as public_mcp_servers,
  COALESCE(tm.private_mcp_servers, 0) as private_mcp_servers,
  COALESCE(tm.total_enabled_servers, 0) as total_enabled_servers,
  COALESCE(tm.total_toolsets, 0) as total_toolsets,
  COALESCE(tlm.total_tools, 0) as total_tools
FROM toolset_metrics tm
FULL OUTER JOIN tool_metrics tlm ON tm.organization_id = tlm.organization_id;

-- name: GetAllOrganizationsWithToolsets :many
SELECT
    organization_metadata.id,
    organization_metadata.name,
    organization_metadata.slug,
    gram_account_type
FROM organization_metadata
JOIN toolsets ON organization_metadata.id = toolsets.organization_id
WHERE toolsets.deleted = false
GROUP BY organization_metadata.id
HAVING COUNT(toolsets.id) > 0;

-- name: GetUserEmailsByOrgIDs :many
-- Get user emails for organization IDs by looking up the latest deployment for each org
SELECT DISTINCT
    d.organization_id,
    u.email
FROM deployments d
JOIN users u ON d.user_id = u.id
WHERE d.organization_id = ANY($1::text[])
  AND d.id IN (
    SELECT DISTINCT ON (organization_id) id
    FROM deployments 
    WHERE organization_id = ANY($1::text[])
    ORDER BY organization_id, created_at DESC
  );
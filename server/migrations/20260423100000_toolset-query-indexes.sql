-- Add indexes to improve toolset listing query performance.
-- These support the batch queries used by mv.GetToolsetsSummary
-- (listToolsetsForOrg endpoint) and mv.DescribeToolsetEntry.

-- http_tool_definitions: FindHttpToolEntriesByUrn filters on
-- (deployment_id, tool_urn) but existing index only covers
-- (deployment_id, deleted, id). Adding tool_urn avoids a full
-- scan of all tools within a deployment.
CREATE INDEX CONCURRENTLY IF NOT EXISTS http_tool_definitions_deployment_tool_urn_idx
ON http_tool_definitions (deployment_id, tool_urn)
WHERE deleted IS FALSE;

-- prompt_templates: PeekTemplatesByUrns filters on
-- (project_id, tool_urn) but no existing index covers this.
CREATE INDEX CONCURRENTLY IF NOT EXISTS prompt_templates_project_id_tool_urn_idx
ON prompt_templates (project_id, tool_urn)
WHERE deleted IS FALSE;

-- external_mcp_tool_definitions: GetExternalMCPToolDefinitionsByURNs
-- filters on tool_urn but existing index only covers
-- (external_mcp_attachment_id). Adding tool_urn avoids a seq scan
-- after the join.
CREATE INDEX CONCURRENTLY IF NOT EXISTS external_mcp_tool_definitions_tool_urn_idx
ON external_mcp_tool_definitions (tool_urn)
WHERE deleted IS FALSE;

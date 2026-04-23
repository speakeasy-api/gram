-- atlas:txmode none

CREATE INDEX CONCURRENTLY IF NOT EXISTS external_mcp_tool_definitions_tool_urn_idx
ON external_mcp_tool_definitions (tool_urn)
WHERE deleted IS FALSE;

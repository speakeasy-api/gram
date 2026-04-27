-- atlas:txmode none

CREATE INDEX CONCURRENTLY IF NOT EXISTS http_tool_definitions_deployment_tool_urn_idx
ON http_tool_definitions (deployment_id, tool_urn)
WHERE deleted IS FALSE;

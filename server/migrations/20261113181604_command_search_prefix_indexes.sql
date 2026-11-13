-- atlas:txmode none

-- Create index "environments_project_id_lower_name_idx" to table: "environments"
CREATE INDEX CONCURRENTLY "environments_project_id_lower_name_idx" ON "environments" ("project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "meta_mcp_servers_organization_project_lower_name_idx" to table: "meta_mcp_servers"
CREATE INDEX CONCURRENTLY "meta_mcp_servers_organization_project_lower_name_idx" ON "meta_mcp_servers" ("organization_id", "project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "plugins_organization_project_lower_name_idx" to table: "plugins"
CREATE INDEX CONCURRENTLY "plugins_organization_project_lower_name_idx" ON "plugins" ("organization_id", "project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "risk_custom_rules_org_project_lower_title_idx" to table: "risk_custom_detection_rules"
CREATE INDEX CONCURRENTLY "risk_custom_rules_org_project_lower_title_idx" ON "risk_custom_detection_rules" ("organization_id", "project_id", (lower(title))) WHERE (deleted IS FALSE);

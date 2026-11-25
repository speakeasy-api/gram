-- atlas:txmode none

-- Create index "assistants_project_id_lower_name_idx" to table: "assistants"
CREATE INDEX CONCURRENTLY "assistants_project_id_lower_name_idx" ON "assistants" ("project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "environments_project_id_lower_name_idx" to table: "environments"
CREATE INDEX CONCURRENTLY "environments_project_id_lower_name_idx" ON "environments" ("project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "mcp_servers_project_id_lower_name_listable_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_project_id_lower_name_listable_idx" ON "mcp_servers" ("project_id", (lower(name))) WHERE ((deleted IS FALSE) AND (toolset_id IS NULL) AND (builtin_backend IS NULL));
-- Create index "meta_mcp_servers_organization_project_lower_name_idx" to table: "meta_mcp_servers"
CREATE INDEX CONCURRENTLY "meta_mcp_servers_organization_project_lower_name_idx" ON "meta_mcp_servers" ("organization_id", "project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "plugins_organization_project_lower_name_idx" to table: "plugins"
CREATE INDEX CONCURRENTLY "plugins_organization_project_lower_name_idx" ON "plugins" ("organization_id", "project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "risk_custom_rules_org_project_lower_title_idx" to table: "risk_custom_detection_rules"
CREATE INDEX CONCURRENTLY "risk_custom_rules_org_project_lower_title_idx" ON "risk_custom_detection_rules" ("organization_id", "project_id", (lower(title))) WHERE (deleted IS FALSE);
-- Create index "skills_project_id_lower_display_name_idx" to table: "skills"
CREATE INDEX CONCURRENTLY "skills_project_id_lower_display_name_idx" ON "skills" ("project_id", (lower(display_name))) WHERE (archived_at IS NULL);
-- Create index "toolsets_project_id_lower_name_idx" to table: "toolsets"
CREATE INDEX CONCURRENTLY "toolsets_project_id_lower_name_idx" ON "toolsets" ("project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "users_display_name_lower_active_idx" to table: "users"
CREATE INDEX CONCURRENTLY "users_display_name_lower_active_idx" ON "users" ((lower(display_name))) WHERE (deleted_at IS NULL);

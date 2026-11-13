-- atlas:txmode none

-- Create index "assistants_project_id_lower_name_idx" to table: "assistants"
CREATE INDEX CONCURRENTLY "assistants_project_id_lower_name_idx" ON "assistants" ("project_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "mcp_servers_project_id_lower_name_listable_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_project_id_lower_name_listable_idx" ON "mcp_servers" ("project_id", (lower(name))) WHERE ((deleted IS FALSE) AND (toolset_id IS NULL) AND (builtin_backend IS NULL) AND (num_nonnulls(remote_mcp_server_id, tunneled_mcp_server_id, unproxied_mcp_server_id) > 0));
-- Create index "skills_project_id_lower_display_name_idx" to table: "skills"
CREATE INDEX CONCURRENTLY "skills_project_id_lower_display_name_idx" ON "skills" ("project_id", (lower(display_name))) WHERE (archived_at IS NULL);
-- Create index "toolsets_project_id_lower_name_idx" to table: "toolsets"
CREATE INDEX CONCURRENTLY "toolsets_project_id_lower_name_idx" ON "toolsets" ("project_id", (lower(name))) WHERE (deleted IS FALSE);

-- atlas:txmode none

-- Modify "plugin_servers" table
ALTER TABLE "plugin_servers" DROP CONSTRAINT "plugin_servers_backend_exclusivity_check", ADD CONSTRAINT "plugin_servers_backend_exclusivity_check" CHECK ((num_nonnulls(toolset_id, mcp_server_id, meta_mcp_server_id) = 1) OR ((deleted_at IS NOT NULL) AND (num_nonnulls(toolset_id, mcp_server_id, meta_mcp_server_id) = 0))) NOT VALID, ADD CONSTRAINT "plugin_servers_gateway_project_check" CHECK ((meta_mcp_server_id IS NULL) OR (project_id IS NOT NULL)) NOT VALID, ADD COLUMN "project_id" uuid NULL, ADD COLUMN "meta_mcp_server_id" uuid NULL, ADD CONSTRAINT "plugin_servers_project_id_meta_mcp_server_id_fkey" FOREIGN KEY ("project_id", "meta_mcp_server_id") REFERENCES "meta_mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID, ADD CONSTRAINT "plugin_servers_project_id_plugin_id_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "plugin_servers" VALIDATE CONSTRAINT "plugin_servers_backend_exclusivity_check";
ALTER TABLE "plugin_servers" VALIDATE CONSTRAINT "plugin_servers_gateway_project_check";
ALTER TABLE "plugin_servers" VALIDATE CONSTRAINT "plugin_servers_project_id_meta_mcp_server_id_fkey";
ALTER TABLE "plugin_servers" VALIDATE CONSTRAINT "plugin_servers_project_id_plugin_id_fkey";
-- Create index "plugin_servers_meta_mcp_server_id_idx" to table: "plugin_servers"
CREATE INDEX CONCURRENTLY "plugin_servers_meta_mcp_server_id_idx" ON "plugin_servers" ("meta_mcp_server_id");
-- Create index "plugin_servers_plugin_id_meta_mcp_server_id_key" to table: "plugin_servers"
CREATE UNIQUE INDEX CONCURRENTLY "plugin_servers_plugin_id_meta_mcp_server_id_key" ON "plugin_servers" ("plugin_id", "meta_mcp_server_id") WHERE (deleted IS FALSE);

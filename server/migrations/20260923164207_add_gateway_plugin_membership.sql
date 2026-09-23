-- atlas:txmode none

-- Modify "plugin_servers" table
ALTER TABLE "plugin_servers" DROP CONSTRAINT "plugin_servers_backend_exclusivity_check", ADD CONSTRAINT "plugin_servers_backend_exclusivity_check" CHECK ((num_nonnulls(toolset_id, mcp_server_id, meta_mcp_server_id) = 1) OR ((deleted_at IS NOT NULL) AND (num_nonnulls(toolset_id, mcp_server_id, meta_mcp_server_id) = 0))) NOT VALID, ADD COLUMN "meta_mcp_server_id" uuid NULL, ADD CONSTRAINT "plugin_servers_meta_mcp_server_id_fkey" FOREIGN KEY ("meta_mcp_server_id") REFERENCES "meta_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID;
ALTER TABLE "plugin_servers" VALIDATE CONSTRAINT "plugin_servers_backend_exclusivity_check";
ALTER TABLE "plugin_servers" VALIDATE CONSTRAINT "plugin_servers_meta_mcp_server_id_fkey";
-- Create index "plugin_servers_plugin_id_meta_mcp_server_id_key" to table: "plugin_servers"
CREATE UNIQUE INDEX CONCURRENTLY "plugin_servers_plugin_id_meta_mcp_server_id_key" ON "plugin_servers" ("plugin_id", "meta_mcp_server_id") WHERE (deleted IS FALSE);

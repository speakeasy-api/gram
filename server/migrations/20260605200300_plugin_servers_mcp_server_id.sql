-- atlas:txmode none

-- Modify "plugin_servers" table
ALTER TABLE "plugin_servers" ADD CONSTRAINT "plugin_servers_backend_exclusivity_check" CHECK ((toolset_id IS NULL) <> (mcp_server_id IS NULL)), ALTER COLUMN "toolset_id" DROP NOT NULL, ADD COLUMN "mcp_server_id" uuid NULL, ADD CONSTRAINT "plugin_servers_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Create index "plugin_servers_plugin_id_mcp_server_id_key" to table: "plugin_servers"
CREATE UNIQUE INDEX CONCURRENTLY "plugin_servers_plugin_id_mcp_server_id_key" ON "plugin_servers" ("plugin_id", "mcp_server_id") WHERE (deleted IS FALSE);

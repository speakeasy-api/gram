-- atlas:txmode none

-- Create index "plugin_servers_mcp_server_id_idx" to table: "plugin_servers"
CREATE INDEX CONCURRENTLY "plugin_servers_mcp_server_id_idx" ON "plugin_servers" ("mcp_server_id");

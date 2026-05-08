-- atlas:txmode none

-- Create index "mcp_servers_remote_mcp_server_id_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_remote_mcp_server_id_idx" ON "mcp_servers" ("remote_mcp_server_id") WHERE (remote_mcp_server_id IS NOT NULL);
-- Create index "mcp_servers_toolset_id_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_toolset_id_idx" ON "mcp_servers" ("toolset_id") WHERE (toolset_id IS NOT NULL);

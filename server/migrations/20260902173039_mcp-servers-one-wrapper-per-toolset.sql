-- atlas:txmode none

-- Create index "mcp_servers_toolset_id_key" to table: "mcp_servers"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_servers_toolset_id_key" ON "mcp_servers" ("toolset_id") WHERE ((toolset_id IS NOT NULL) AND (deleted IS FALSE));

-- atlas:txmode none

-- Create index "remote_mcp_server_headers_remote_mcp_server_id_history_idx" to table: "remote_mcp_server_headers"
CREATE INDEX CONCURRENTLY "remote_mcp_server_headers_remote_mcp_server_id_history_idx" ON "remote_mcp_server_headers" ("remote_mcp_server_id");

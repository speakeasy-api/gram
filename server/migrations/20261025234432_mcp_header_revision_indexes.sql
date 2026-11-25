-- atlas:txmode none

-- Create index "mcp_auth_client_headers_auth_client_id_history_idx" to table: "mcp_auth_client_headers"
CREATE INDEX CONCURRENTLY "mcp_auth_client_headers_auth_client_id_history_idx" ON "mcp_auth_client_headers" ("auth_client_id");
-- Create index "remote_mcp_server_headers_remote_mcp_server_id_history_idx" to table: "remote_mcp_server_headers"
CREATE INDEX CONCURRENTLY "remote_mcp_server_headers_remote_mcp_server_id_history_idx" ON "remote_mcp_server_headers" ("remote_mcp_server_id");

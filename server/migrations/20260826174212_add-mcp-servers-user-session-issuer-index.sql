-- atlas:txmode none

-- Create index "mcp_servers_user_session_issuer_id_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_user_session_issuer_id_idx" ON "mcp_servers" ("user_session_issuer_id") WHERE (user_session_issuer_id IS NOT NULL);

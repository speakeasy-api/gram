-- atlas:txmode none

-- Create index "mcp_auth_client_headers_auth_client_id_organization_id_idx" to table: "mcp_auth_client_headers"
CREATE INDEX CONCURRENTLY "mcp_auth_client_headers_auth_client_id_organization_id_idx" ON "mcp_auth_client_headers" ("auth_client_id", "organization_id");

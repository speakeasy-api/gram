-- atlas:txmode none

-- Add the issuer-required constraint without validating existing rows while
-- holding the table lock, then validate it separately without blocking writes.
ALTER TABLE "mcp_servers" ADD CONSTRAINT "mcp_servers_issuer_required_check" CHECK (((remote_mcp_server_id IS NULL) AND (tunneled_mcp_server_id IS NULL)) OR (user_session_issuer_id IS NOT NULL)) NOT VALID;
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_issuer_required_check";

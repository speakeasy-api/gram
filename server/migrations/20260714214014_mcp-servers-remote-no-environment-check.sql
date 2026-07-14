-- atlas:txmode none

-- Add the remote-no-environment constraint without validating existing rows
-- while holding the table lock, then validate it separately without
-- blocking writes. Mirrors the mcp_servers_issuer_required_check migration.
ALTER TABLE "mcp_servers" ADD CONSTRAINT "mcp_servers_remote_no_environment_check" CHECK ((environment_id IS NULL) OR (remote_mcp_server_id IS NULL)) NOT VALID;
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_remote_no_environment_check";

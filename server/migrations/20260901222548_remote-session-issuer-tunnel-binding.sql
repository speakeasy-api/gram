-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "tunneled_mcp_server_id" uuid NULL, ADD CONSTRAINT "remote_session_issuers_tunneled_mcp_server_id_fkey" FOREIGN KEY ("tunneled_mcp_server_id") REFERENCES "tunneled_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;

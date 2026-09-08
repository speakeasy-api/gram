-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "metadata" jsonb NULL;
-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" DROP CONSTRAINT "mcp_servers_issuer_required_check", ADD CONSTRAINT "mcp_servers_authorization_exclusivity_check" CHECK (num_nonnulls(user_session_issuer_id, remote_session_issuer_id) <= 1), ADD COLUMN "remote_session_issuer_id" uuid NULL, ADD CONSTRAINT "mcp_servers_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;

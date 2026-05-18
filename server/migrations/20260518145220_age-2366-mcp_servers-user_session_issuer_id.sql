-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" DROP COLUMN "external_oauth_server_id", DROP COLUMN "oauth_proxy_server_id", ADD COLUMN "user_session_issuer_id" uuid NULL, ADD CONSTRAINT "mcp_servers_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;

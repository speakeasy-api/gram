-- atlas:txmode none

-- Create index "user_session_issuers_organization_id_id_key" to table: "user_session_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "user_session_issuers_organization_id_id_key" ON "user_session_issuers" ("organization_id", "id");
-- Modify "meta_mcp_servers" table
ALTER TABLE "meta_mcp_servers" ADD CONSTRAINT "meta_mcp_servers_organization_id_user_session_issuer_id_fkey" FOREIGN KEY ("organization_id", "user_session_issuer_id") REFERENCES "user_session_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Modify "platform_mcp_catalog_registrations" table
ALTER TABLE "platform_mcp_catalog_registrations" ADD CONSTRAINT "platform_mcp_catalog_registrations_org_session_issuer_fkey" FOREIGN KEY ("organization_id", "user_session_issuer_id") REFERENCES "user_session_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION;

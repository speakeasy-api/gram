-- atlas:txmode none

-- Modify "mcp_endpoints" table
ALTER TABLE "mcp_endpoints" ADD CONSTRAINT "mcp_endpoints_backend_exclusivity_check" CHECK (num_nonnulls(mcp_server_id, plugin_id) = 1) NOT VALID, ADD CONSTRAINT "mcp_endpoints_gateway_issuer_required_check" CHECK ((plugin_id IS NULL) OR (user_session_issuer_id IS NOT NULL)) NOT VALID, ALTER COLUMN "mcp_server_id" DROP NOT NULL, ADD COLUMN "plugin_id" uuid NULL, ADD COLUMN "user_session_issuer_id" uuid NULL, ADD COLUMN "disabled" boolean NOT NULL DEFAULT false, ADD CONSTRAINT "mcp_endpoints_project_id_plugin_id_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT, ADD CONSTRAINT "mcp_endpoints_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
ALTER TABLE "mcp_endpoints" VALIDATE CONSTRAINT "mcp_endpoints_backend_exclusivity_check";
ALTER TABLE "mcp_endpoints" VALIDATE CONSTRAINT "mcp_endpoints_gateway_issuer_required_check";
-- Create index "mcp_endpoints_plugin_id_key" to table: "mcp_endpoints"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_endpoints_plugin_id_key" ON "mcp_endpoints" ("plugin_id") WHERE ((plugin_id IS NOT NULL) AND (deleted IS FALSE));

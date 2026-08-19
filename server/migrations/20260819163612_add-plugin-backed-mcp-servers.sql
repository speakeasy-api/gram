-- atlas:txmode none

-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" DROP CONSTRAINT "mcp_servers_backend_exclusivity_check", ADD CONSTRAINT "mcp_servers_backend_exclusivity_check" CHECK (num_nonnulls(remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, unproxied_mcp_server_id, plugin_id) = 1) NOT VALID, DROP CONSTRAINT "mcp_servers_issuer_required_check", ADD CONSTRAINT "mcp_servers_issuer_required_check" CHECK (deleted OR ((remote_mcp_server_id IS NULL) AND (tunneled_mcp_server_id IS NULL) AND (plugin_id IS NULL)) OR (user_session_issuer_id IS NOT NULL)) NOT VALID, ADD CONSTRAINT "mcp_servers_plugin_member_deadline_check" CHECK (((plugin_id IS NULL) AND (member_deadline_ms IS NULL)) OR ((plugin_id IS NOT NULL) AND (member_deadline_ms IS NOT NULL) AND (member_deadline_ms > 0))) NOT VALID, ADD COLUMN "plugin_id" uuid NULL, ADD COLUMN "member_deadline_ms" integer NULL, ADD CONSTRAINT "mcp_servers_project_id_plugin_id_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT NOT VALID;
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_backend_exclusivity_check";
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_issuer_required_check";
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_plugin_member_deadline_check";
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_project_id_plugin_id_fkey";
-- Create index "mcp_servers_project_id_plugin_id_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_project_id_plugin_id_idx" ON "mcp_servers" ("project_id", "plugin_id") WHERE (plugin_id IS NOT NULL);
-- Set comment to column: "tunneled_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tunneled_mcp_server_id" IS 'Optional backend reference to a tunneled MCP source. Exactly one backend reference must be set.';

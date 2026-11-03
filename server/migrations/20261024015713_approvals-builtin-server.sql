-- atlas:txmode none

-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" DROP CONSTRAINT "mcp_servers_backend_exclusivity_check", ADD CONSTRAINT "mcp_servers_backend_exclusivity_check" CHECK (num_nonnulls(remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, unproxied_mcp_server_id, builtin_backend) = 1), ADD COLUMN "builtin_backend" text NULL;
-- Create index "mcp_servers_project_id_builtin_backend_key" to table: "mcp_servers"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_servers_project_id_builtin_backend_key" ON "mcp_servers" ("project_id", "builtin_backend") WHERE ((builtin_backend IS NOT NULL) AND (deleted IS FALSE));
-- Set comment to column: "tunneled_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tunneled_mcp_server_id" IS 'Optional backend reference to a tunneled MCP source. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, unproxied_mcp_server_id, or builtin_backend must be set.
@access: confidential';
-- Set comment to column: "builtin_backend" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."builtin_backend" IS '@access: confidential';

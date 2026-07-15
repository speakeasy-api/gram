-- Modify "tunneled_mcp_servers" table
ALTER TABLE "tunneled_mcp_servers" ADD COLUMN "allow_public" boolean NOT NULL DEFAULT false;
-- Set comment to column: "allow_public" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."allow_public" IS 'Owner consent for anonymous public MCP serving of this source. Double opt-in with mcp_servers.visibility=public, enforced in application code.';

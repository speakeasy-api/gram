-- Modify "projects" table
ALTER TABLE "projects" ADD COLUMN "require_mcp_gateway" boolean NULL;
-- Set comment to column: "require_mcp_gateway" on table: "projects"
COMMENT ON COLUMN "projects"."require_mcp_gateway" IS 'Require MCP connections through a gateway. NULL and false allow direct connections.
@access: confidential';

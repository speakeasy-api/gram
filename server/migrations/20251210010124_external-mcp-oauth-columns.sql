-- Modify "external_mcp_tool_definitions" table
ALTER TABLE "external_mcp_tool_definitions" ADD COLUMN "requires_oauth" boolean NOT NULL DEFAULT false, ADD COLUMN "authenticate_header" text NULL;

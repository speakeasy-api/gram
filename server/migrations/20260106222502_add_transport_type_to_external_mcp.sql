-- Modify "external_mcp_tool_definitions" table
ALTER TABLE "external_mcp_tool_definitions" ADD COLUMN "transport_type" text NOT NULL DEFAULT 'streamable-http';

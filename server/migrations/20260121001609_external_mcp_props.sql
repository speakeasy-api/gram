-- Modify "external_mcp_tool_definitions" table
ALTER TABLE "external_mcp_tool_definitions" ADD COLUMN "type" text NOT NULL DEFAULT 'proxy', ADD COLUMN "name" text NULL, ADD COLUMN "description" text NULL, ADD COLUMN "schema" jsonb NULL;

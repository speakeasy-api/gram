-- Modify "external_mcp_tool_definitions" table
ALTER TABLE "external_mcp_tool_definitions" ADD COLUMN "title" text NULL, ADD COLUMN "read_only_hint" boolean NULL, ADD COLUMN "destructive_hint" boolean NULL, ADD COLUMN "idempotent_hint" boolean NULL, ADD COLUMN "open_world_hint" boolean NULL;

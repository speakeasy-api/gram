-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ADD COLUMN "tool_urn" text NULL;
-- Modify "prompt_templates" table
ALTER TABLE "prompt_templates" ADD COLUMN "tool_urn" text NULL;

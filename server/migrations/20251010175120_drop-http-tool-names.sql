-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ALTER COLUMN "tool_urn" SET NOT NULL;
-- Modify "prompt_templates" table
ALTER TABLE "prompt_templates" ALTER COLUMN "tool_urn" SET NOT NULL;
-- Modify "toolsets" table
ALTER TABLE "toolsets" DROP CONSTRAINT "toolsets_http_tool_names_check", DROP COLUMN "http_tool_names";

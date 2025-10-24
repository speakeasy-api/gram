-- Modify "function_resource_definitions" table
ALTER TABLE "function_resource_definitions" ADD COLUMN "meta" jsonb NULL;
-- Modify "function_tool_definitions" table
ALTER TABLE "function_tool_definitions" ADD COLUMN "meta" jsonb NULL;

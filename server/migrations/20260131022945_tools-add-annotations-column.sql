-- Modify "function_tool_definitions" table
ALTER TABLE "function_tool_definitions" ADD COLUMN "annotations" jsonb NULL;
-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ADD COLUMN "annotations" jsonb NULL;

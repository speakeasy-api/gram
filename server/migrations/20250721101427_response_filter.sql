-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ADD COLUMN "response_filter" jsonb NULL;

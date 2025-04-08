-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ADD COLUMN "header_settings" jsonb NULL, ADD COLUMN "query_settings" jsonb NULL, ADD COLUMN "path_settings" jsonb NULL;

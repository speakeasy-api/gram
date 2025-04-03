-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ADD COLUMN "tags" text[] NOT NULL DEFAULT ARRAY[]::text[];

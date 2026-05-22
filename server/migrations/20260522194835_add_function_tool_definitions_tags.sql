-- Modify "function_tool_definitions" table
ALTER TABLE "function_tool_definitions" ADD CONSTRAINT "function_tool_definitions_tags_check" CHECK (array_length(tags, 1) <= 40), ADD COLUMN "tags" text[] NOT NULL DEFAULT ARRAY[]::text[];

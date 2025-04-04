-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" DROP CONSTRAINT "http_tool_definitions_project_id_name_key", DROP COLUMN "organization_id", ADD COLUMN "summary" text NOT NULL, ADD COLUMN "openapiv3_operation" text NULL;
-- Create index "http_tool_definitions_name_idx" to table: "http_tool_definitions"
CREATE INDEX "http_tool_definitions_name_idx" ON "http_tool_definitions" ("name");

-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ADD CONSTRAINT "http_tool_definitions_project_id_name_key" UNIQUE ("project_id", "name");

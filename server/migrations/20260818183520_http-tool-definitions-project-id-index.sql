-- atlas:txmode none

-- Create index "http_tool_definitions_project_id_deleted_idx" to table: "http_tool_definitions"
CREATE INDEX CONCURRENTLY "http_tool_definitions_project_id_deleted_idx" ON "http_tool_definitions" ("project_id", "deleted");

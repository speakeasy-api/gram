-- atlas:txmode none

-- Create index "deployments_project_id_seq_idx" to table: "deployments"
CREATE INDEX CONCURRENTLY "deployments_project_id_seq_idx" ON "deployments" ("project_id", "seq" DESC);
-- Create index "http_tool_definitions_deployment_deleted_id_idx" to table: "http_tool_definitions"
CREATE INDEX CONCURRENTLY "http_tool_definitions_deployment_deleted_id_idx" ON "http_tool_definitions" ("deployment_id", "deleted", "id" DESC) WHERE (deleted IS FALSE);

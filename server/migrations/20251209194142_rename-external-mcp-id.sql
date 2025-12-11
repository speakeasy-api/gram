-- atlas:txmode none

-- Modify "external_mcp_tool_definitions" table
ALTER TABLE "external_mcp_tool_definitions" DROP COLUMN "external_mcp_id", ADD COLUMN "deployment_external_mcp_id" uuid NOT NULL, ADD CONSTRAINT "external_mcp_tool_definitions_deployment_external_mcp_id_fkey" FOREIGN KEY ("deployment_external_mcp_id") REFERENCES "deployments_external_mcps" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "external_mcp_tool_definitions_deployment_external_mcp_id_idx" to table: "external_mcp_tool_definitions"
CREATE INDEX CONCURRENTLY "external_mcp_tool_definitions_deployment_external_mcp_id_idx" ON "external_mcp_tool_definitions" ("deployment_external_mcp_id") WHERE (deleted IS FALSE);

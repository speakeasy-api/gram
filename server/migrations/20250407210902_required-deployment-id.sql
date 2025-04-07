-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" DROP CONSTRAINT "http_tool_definitions_deployment_id_fkey", ALTER COLUMN "deployment_id" SET NOT NULL, ADD CONSTRAINT "http_tool_definitions_deployment_id_fkey" FOREIGN KEY ("deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;

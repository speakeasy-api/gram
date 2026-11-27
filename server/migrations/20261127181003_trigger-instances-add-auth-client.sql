-- atlas:txmode none

-- Modify "trigger_instances" table
ALTER TABLE "trigger_instances" ADD COLUMN "auth_client_id" uuid NULL, ADD CONSTRAINT "trigger_instances_auth_client_fkey" FOREIGN KEY ("auth_client_id", "organization_id") REFERENCES "mcp_auth_clients" ("id", "organization_id") ON UPDATE NO ACTION ON DELETE SET NULL ("auth_client_id") NOT VALID;
ALTER TABLE "trigger_instances" VALIDATE CONSTRAINT "trigger_instances_auth_client_fkey";
-- Create index "trigger_instances_auth_client_id_idx" to table: "trigger_instances"
CREATE INDEX CONCURRENTLY "trigger_instances_auth_client_id_idx" ON "trigger_instances" ("auth_client_id", "organization_id");
-- Set comment to column: "auth_client_id" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."auth_client_id" IS '@access: confidential';

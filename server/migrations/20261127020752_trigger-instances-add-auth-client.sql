-- atlas:txmode none

-- Modify "trigger_instances" table
ALTER TABLE "trigger_instances" ADD CONSTRAINT "trigger_instances_auth_source_check" CHECK ((environment_id IS NULL) OR (auth_client_id IS NULL)) NOT VALID, ADD COLUMN "auth_client_id" uuid NULL, ADD CONSTRAINT "trigger_instances_auth_client_fkey" FOREIGN KEY ("auth_client_id", "organization_id") REFERENCES "mcp_auth_clients" ("id", "organization_id") ON UPDATE NO ACTION ON DELETE SET NULL ("auth_client_id") NOT VALID;
ALTER TABLE "trigger_instances" VALIDATE CONSTRAINT "trigger_instances_auth_source_check";
ALTER TABLE "trigger_instances" VALIDATE CONSTRAINT "trigger_instances_auth_client_fkey";
-- Create index "trigger_instances_auth_client_id_idx" to table: "trigger_instances"
CREATE INDEX CONCURRENTLY "trigger_instances_auth_client_id_idx" ON "trigger_instances" ("organization_id", "auth_client_id") WHERE ((auth_client_id IS NOT NULL) AND (deleted IS FALSE));
-- Set comment to column: "auth_client_id" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."auth_client_id" IS '@access: confidential';

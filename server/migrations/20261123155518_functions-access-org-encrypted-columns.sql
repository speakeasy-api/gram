-- atlas:txmode none

-- Modify "functions_access" table
ALTER TABLE "functions_access" ADD COLUMN "organization_id" text NULL, ADD COLUMN "encryption_key_org_encrypted" text NULL, ADD CONSTRAINT "functions_access_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID, ADD CONSTRAINT "functions_access_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "functions_access" VALIDATE CONSTRAINT "functions_access_organization_id_fkey";
ALTER TABLE "functions_access" VALIDATE CONSTRAINT "functions_access_project_tenant_fkey";
-- Create index "functions_access_organization_id_id_idx" to table: "functions_access"
CREATE INDEX CONCURRENTLY "functions_access_organization_id_id_idx" ON "functions_access" ("organization_id", "id") WHERE (encryption_key IS NOT NULL);
-- Create index "functions_access_project_id_unattributed_idx" to table: "functions_access"
CREATE INDEX CONCURRENTLY "functions_access_project_id_unattributed_idx" ON "functions_access" ("project_id") WHERE (organization_id IS NULL);
-- Set comment to column: "organization_id" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."organization_id" IS '@access: confidential';
-- Set comment to column: "encryption_key_org_encrypted" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."encryption_key_org_encrypted" IS '@access: secret-restricted';

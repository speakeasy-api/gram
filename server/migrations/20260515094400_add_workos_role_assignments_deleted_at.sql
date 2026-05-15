-- atlas:txmode none

-- Drop index "organization_role_assignments_org_workos_user_role_key" from table: "organization_role_assignments"
DROP INDEX CONCURRENTLY "organization_role_assignments_org_workos_user_role_key";
-- Modify "organization_role_assignments" table
ALTER TABLE "organization_role_assignments" ADD COLUMN "deleted_at" timestamptz NULL;
-- Create index "organization_role_assignments_org_workos_user_role_key" to table: "organization_role_assignments"
CREATE UNIQUE INDEX CONCURRENTLY "organization_role_assignments_org_workos_user_role_key" ON "organization_role_assignments" ("organization_id", "workos_user_id", "role_urn") WHERE (deleted_at IS NULL);

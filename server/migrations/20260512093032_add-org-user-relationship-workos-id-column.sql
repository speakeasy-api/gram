-- atlas:txmode none

-- Modify "organization_user_relationships" table
ALTER TABLE "organization_user_relationships" ADD COLUMN "workos_user_id" text NULL;
-- Create index "organization_user_relationships_org_workos_user_idx" to table: "organization_user_relationships"
CREATE INDEX CONCURRENTLY "organization_user_relationships_org_workos_user_idx" ON "organization_user_relationships" ("organization_id", "workos_user_id") WHERE ((workos_user_id IS NOT NULL) AND (deleted IS FALSE));

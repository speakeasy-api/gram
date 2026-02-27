-- atlas:txmode none

-- Modify "organization_user_relationships" table
ALTER TABLE "organization_user_relationships" ADD COLUMN "workos_membership_id" text NULL;
-- Create index "organization_user_relationships_workos_membership_id_key" to table: "organization_user_relationships"
CREATE UNIQUE INDEX CONCURRENTLY "organization_user_relationships_workos_membership_id_key" ON "organization_user_relationships" ("workos_membership_id") WHERE (deleted IS FALSE);
-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "workos_id" text NULL;
-- Create index "users_workos_id_key" to table: "users"
CREATE UNIQUE INDEX CONCURRENTLY "users_workos_id_key" ON "users" ("workos_id");

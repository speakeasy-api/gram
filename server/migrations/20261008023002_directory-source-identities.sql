-- atlas:txmode none

-- Modify "directory_groups" table
ALTER TABLE "directory_groups" ADD COLUMN "directory_id" text NULL;
-- Create index "directory_groups_organization_id_directory_id_idx" to table: "directory_groups"
CREATE INDEX CONCURRENTLY "directory_groups_organization_id_directory_id_idx" ON "directory_groups" ("organization_id", "directory_id") WHERE (directory_id IS NOT NULL);
-- Set comment to column: "directory_id" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."directory_id" IS 'WorkOS directory ID. NULL until an authoritative directory inventory or entity payload attributes this source.';
-- Modify "directory_users" table
ALTER TABLE "directory_users" ADD COLUMN "directory_id" text NULL;
-- Create index "directory_users_organization_id_directory_id_idx" to table: "directory_users"
CREATE INDEX CONCURRENTLY "directory_users_organization_id_directory_id_idx" ON "directory_users" ("organization_id", "directory_id") WHERE (directory_id IS NOT NULL);
-- Set comment to column: "directory_id" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."directory_id" IS 'WorkOS directory ID. NULL until an authoritative directory inventory or entity payload attributes this source.';

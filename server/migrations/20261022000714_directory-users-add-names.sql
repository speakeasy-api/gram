-- Modify "directory_users" table
ALTER TABLE "directory_users" ADD COLUMN "first_name" text NULL, ADD COLUMN "last_name" text NULL;
-- Set comment to column: "first_name" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."first_name" IS 'Given name from the WorkOS directory user. NULL until a dsync.user event or the name backfill provides it.
@access: confidential-pii';
-- Set comment to column: "last_name" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."last_name" IS 'Family name from the WorkOS directory user. NULL until a dsync.user event or the name backfill provides it.
@access: confidential-pii';

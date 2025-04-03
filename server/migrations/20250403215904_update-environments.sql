-- Modify "environment_entries" table
ALTER TABLE "environment_entries" DROP COLUMN "deleted", DROP COLUMN "deleted_at";
-- Modify "environments" table
ALTER TABLE "environments" ADD COLUMN "description" text NULL;

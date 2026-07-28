-- Modify "environment_entries" table
ALTER TABLE "environment_entries" ADD COLUMN "is_secret" boolean NOT NULL DEFAULT true;

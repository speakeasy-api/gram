-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "disabled_at" timestamptz NULL;

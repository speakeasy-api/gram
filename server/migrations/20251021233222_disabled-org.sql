-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "disabled" boolean NOT NULL DEFAULT false;

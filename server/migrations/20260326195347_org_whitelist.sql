-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "whitelisted" boolean NOT NULL DEFAULT true;

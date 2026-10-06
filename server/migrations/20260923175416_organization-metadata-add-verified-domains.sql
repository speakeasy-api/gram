-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "verified_domains" text[] NULL DEFAULT '{}';

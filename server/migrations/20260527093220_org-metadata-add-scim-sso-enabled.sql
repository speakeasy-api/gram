-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "scim_enabled" boolean NULL DEFAULT false, ADD COLUMN "sso_enabled" boolean NULL DEFAULT false;

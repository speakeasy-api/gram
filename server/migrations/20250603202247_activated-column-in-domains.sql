-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD COLUMN "activated" boolean NOT NULL DEFAULT false;

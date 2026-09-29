-- Modify "okta_resource_connections" table
ALTER TABLE "okta_resource_connections" ADD COLUMN "observed_result" text NULL, ADD COLUMN "observed_at" timestamptz NULL;

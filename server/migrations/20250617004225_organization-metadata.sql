-- Create "organization_metadata" table
CREATE TABLE "organization_metadata" ("id" text NOT NULL, "name" text NOT NULL, "slug" text NOT NULL, "account_type" text NOT NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"));

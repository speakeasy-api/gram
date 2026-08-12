-- Create "enterprise_trials" table
CREATE TABLE "enterprise_trials" (
  "organization_id" text NOT NULL,
  "ends_at" timestamptz NOT NULL,
  "converted_at" timestamptz NULL,
  "demoted_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "enterprise_trials_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

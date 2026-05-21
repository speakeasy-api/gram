-- Create "project_marketplace_settings" table
CREATE TABLE "project_marketplace_settings" (
  "project_id" uuid NOT NULL,
  "marketplace_name" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("project_id"),
  CONSTRAINT "project_marketplace_settings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "project_marketplace_settings_marketplace_name_check" CHECK ((marketplace_name IS NULL) OR ((marketplace_name <> ''::text) AND (char_length(marketplace_name) <= 64)))
);

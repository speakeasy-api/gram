-- Create "project_portals" table
CREATE TABLE "project_portals" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "enabled" boolean NOT NULL DEFAULT false,
  "display_name" text NULL,
  "tagline" text NULL,
  "logo_asset_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "project_portals_project_id_key" UNIQUE ("project_id"),
  CONSTRAINT "project_portals_logo_asset_id_fkey" FOREIGN KEY ("logo_asset_id") REFERENCES "assets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "project_portals_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "project_portals_display_name_check" CHECK ((display_name IS NULL) OR ((display_name <> ''::text) AND (char_length(display_name) <= 64))),
  CONSTRAINT "project_portals_tagline_check" CHECK ((tagline IS NULL) OR (char_length(tagline) <= 200))
);

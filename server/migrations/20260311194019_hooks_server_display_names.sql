-- Create "hooks_server_name_overrides" table
CREATE TABLE "hooks_server_name_overrides" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "raw_server_name" text NOT NULL,
  "display_name" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "hooks_overrides_unique_raw" UNIQUE ("project_id", "raw_server_name"),
  CONSTRAINT "hooks_server_name_overrides_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "hooks_server_name_overrides_display_name_check" CHECK (display_name <> ''::text),
  CONSTRAINT "hooks_server_name_overrides_raw_server_name_check" CHECK (raw_server_name <> ''::text)
);
-- Create index "hooks_server_name_overrides_project_id_display_name_idx" to table: "hooks_server_name_overrides"
CREATE INDEX "hooks_server_name_overrides_project_id_display_name_idx" ON "hooks_server_name_overrides" ("project_id", "display_name");

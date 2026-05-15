-- Create "cursor_integration_configs" table
CREATE TABLE "cursor_integration_configs" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "api_key_encrypted" text NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "last_polled_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "cursor_integration_configs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON DELETE CASCADE
);
-- Create index "cursor_integration_configs_org_project_key" to table: "cursor_integration_configs"
CREATE UNIQUE INDEX "cursor_integration_configs_org_project_key" ON "cursor_integration_configs" ("organization_id", "project_id") WHERE (deleted IS FALSE);

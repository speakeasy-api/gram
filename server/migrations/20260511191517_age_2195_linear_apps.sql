-- Create "linear_apps" table
CREATE TABLE "linear_apps" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "linear_workspace_name" text NULL,
  "linear_workspace_id" text NULL,
  "linear_client_secret" text NULL,
  "linear_signing_secret" text NULL,
  "linear_access_token" text NULL,
  "linear_client_id" text NULL,
  "organization_id" text NOT NULL,
  "actor_mode" text NOT NULL DEFAULT 'app',
  "system_prompt" text NULL,
  "name" text NOT NULL,
  "status" text NOT NULL DEFAULT 'unconfigured',
  "icon_asset_id" uuid NULL,
  "project_id" uuid NOT NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "linear_apps_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "linear_apps_linear_workspace_id_idx" to table: "linear_apps"
CREATE INDEX "linear_apps_linear_workspace_id_idx" ON "linear_apps" ("linear_workspace_id") WHERE ((deleted IS FALSE) AND (linear_workspace_id IS NOT NULL));
-- Create index "linear_apps_project_name_key" to table: "linear_apps"
CREATE UNIQUE INDEX "linear_apps_project_name_key" ON "linear_apps" ("project_id", "name") WHERE (deleted IS FALSE);
-- Create "linear_app_toolsets" table
CREATE TABLE "linear_app_toolsets" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "linear_app_id" uuid NOT NULL,
  "toolset_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "linear_app_toolsets_linear_app_id_toolset_id_key" UNIQUE ("linear_app_id", "toolset_id"),
  CONSTRAINT "linear_app_toolsets_linear_app_id_fkey" FOREIGN KEY ("linear_app_id") REFERENCES "linear_apps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "linear_app_toolsets_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "linear_registrations" table
CREATE TABLE "linear_registrations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "linear_app_id" uuid NOT NULL,
  "linear_account_id" text NOT NULL,
  "user_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "linear_registrations_linear_app_id_linear_account_id_key" UNIQUE ("linear_app_id", "linear_account_id"),
  CONSTRAINT "linear_registrations_linear_app_id_fkey" FOREIGN KEY ("linear_app_id") REFERENCES "linear_apps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

-- Create "slack_apps" table
CREATE TABLE "slack_apps" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "slack_team_name" text NULL,
  "slack_bot_user_id" text NULL,
  "slack_client_secret" text NULL,
  "slack_signing_secret" text NULL,
  "slack_team_id" text NULL,
  "organization_id" text NOT NULL,
  "slack_bot_token" text NULL,
  "slack_client_id" text NULL,
  "system_prompt" text NULL,
  "name" text NOT NULL,
  "status" text NOT NULL DEFAULT 'unconfigured',
  "icon_asset_id" uuid NULL,
  "project_id" uuid NOT NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "slack_apps_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "slack_apps_project_name_key" to table: "slack_apps"
CREATE UNIQUE INDEX "slack_apps_project_name_key" ON "slack_apps" ("project_id", "name") WHERE (deleted IS FALSE);
-- Create index "slack_apps_slack_team_id_key" to table: "slack_apps"
CREATE UNIQUE INDEX "slack_apps_slack_team_id_key" ON "slack_apps" ("slack_team_id") WHERE ((deleted IS FALSE) AND (slack_team_id IS NOT NULL));
-- Create "slack_app_toolsets" table
CREATE TABLE "slack_app_toolsets" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slack_app_id" uuid NOT NULL,
  "toolset_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "slack_app_toolsets_slack_app_id_toolset_id_key" UNIQUE ("slack_app_id", "toolset_id"),
  CONSTRAINT "slack_app_toolsets_slack_app_id_fkey" FOREIGN KEY ("slack_app_id") REFERENCES "slack_apps" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "slack_app_toolsets_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

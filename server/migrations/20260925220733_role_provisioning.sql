-- Create "organization_role_provisioning_settings" table
CREATE TABLE "organization_role_provisioning_settings" (
  "organization_id" text NOT NULL,
  "enabled" boolean NULL DEFAULT false,
  "project_id" uuid NULL,
  "version" bigint NULL DEFAULT 0,
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "organization_role_provisioning_settings_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_role_provisioning_settings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create "role_provisioning_settings" table
CREATE TABLE "role_provisioning_settings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NULL,
  "role_urn" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "project_id" uuid NULL,
  "last_attempt_at" timestamptz NULL,
  "last_error_code" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "role_provisioning_settings_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "role_provisioning_settings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "role_provisioning_settings_organization_id_role_urn_key" to table: "role_provisioning_settings"
CREATE UNIQUE INDEX "role_provisioning_settings_organization_id_role_urn_key" ON "role_provisioning_settings" ("organization_id", "role_urn") WHERE (organization_id IS NOT NULL);
-- Create index "role_provisioning_settings_project_id_idx" to table: "role_provisioning_settings"
CREATE INDEX "role_provisioning_settings_project_id_idx" ON "role_provisioning_settings" ("project_id") WHERE (project_id IS NOT NULL);
-- Create "role_plugin_associations" table
CREATE TABLE "role_plugin_associations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "role_provisioning_setting_id" uuid NULL,
  "project_id" uuid NULL,
  "plugin_id" uuid NULL,
  "is_current" boolean NOT NULL DEFAULT false,
  "retired_at" timestamptz NULL,
  "last_automatic_name" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "role_plugin_associations_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "role_plugin_associations_project_id_plugin_id_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "role_plugin_associations_role_provisioning_setting_id_fkey" FOREIGN KEY ("role_provisioning_setting_id") REFERENCES "role_provisioning_settings" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "role_plugin_associations_current_setting_id_key" to table: "role_plugin_associations"
CREATE UNIQUE INDEX "role_plugin_associations_current_setting_id_key" ON "role_plugin_associations" ("role_provisioning_setting_id") WHERE (is_current AND (retired_at IS NULL) AND (role_provisioning_setting_id IS NOT NULL));
-- Create index "role_plugin_associations_plugin_id_key" to table: "role_plugin_associations"
CREATE UNIQUE INDEX "role_plugin_associations_plugin_id_key" ON "role_plugin_associations" ("plugin_id") WHERE (plugin_id IS NOT NULL);
-- Create index "role_plugin_associations_project_id_idx" to table: "role_plugin_associations"
CREATE INDEX "role_plugin_associations_project_id_idx" ON "role_plugin_associations" ("project_id") WHERE (project_id IS NOT NULL);
-- Create index "role_plugin_associations_setting_id_project_id_key" to table: "role_plugin_associations"
CREATE UNIQUE INDEX "role_plugin_associations_setting_id_project_id_key" ON "role_plugin_associations" ("role_provisioning_setting_id", "project_id") WHERE ((retired_at IS NULL) AND (role_provisioning_setting_id IS NOT NULL) AND (project_id IS NOT NULL));

-- Create "device_integration_configs" table
CREATE TABLE "device_integration_configs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "provider" text NOT NULL,
  "credentials_encrypted" text NOT NULL,
  "settings" jsonb NOT NULL DEFAULT '{}',
  "enabled" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "device_integration_configs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "device_integration_configs_organization_id_id_key" to table: "device_integration_configs"
CREATE UNIQUE INDEX "device_integration_configs_organization_id_id_key" ON "device_integration_configs" ("organization_id", "id");
-- Create index "device_integration_configs_organization_id_idx" to table: "device_integration_configs"
CREATE INDEX "device_integration_configs_organization_id_idx" ON "device_integration_configs" ("organization_id");
-- Create index "device_integration_configs_organization_id_provider_key" to table: "device_integration_configs"
CREATE UNIQUE INDEX "device_integration_configs_organization_id_provider_key" ON "device_integration_configs" ("organization_id", "provider") WHERE (deleted IS FALSE);
-- Create "device_integration_schedules" table
CREATE TABLE "device_integration_schedules" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "device_integration_config_id" uuid NOT NULL,
  "schedule" text NOT NULL,
  "disabled_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "device_integration_schedules_device_integration_config_id_fkey" FOREIGN KEY ("device_integration_config_id") REFERENCES "device_integration_configs" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "device_integration_schedules_config_id_schedule_key" to table: "device_integration_schedules"
CREATE UNIQUE INDEX "device_integration_schedules_config_id_schedule_key" ON "device_integration_schedules" ("device_integration_config_id", "schedule");
-- Create "device_integration_syncs" table
CREATE TABLE "device_integration_syncs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "device_integration_schedule_id" uuid NOT NULL,
  "poll_watermark_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "next_poll_after" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_poll_success_at" timestamptz NULL,
  "last_poll_failed_at" timestamptz NULL,
  "last_poll_error" text NULL,
  "consecutive_failures" integer NOT NULL DEFAULT 0,
  "consecutive_auth_rejections" integer NOT NULL DEFAULT 0,
  "last_push_digest" text NULL,
  "auto_paused_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "device_integration_syncs_device_integration_schedule_id_fkey" FOREIGN KEY ("device_integration_schedule_id") REFERENCES "device_integration_schedules" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "device_integration_syncs_next_poll_after_idx" to table: "device_integration_syncs"
CREATE INDEX "device_integration_syncs_next_poll_after_idx" ON "device_integration_syncs" ("next_poll_after") WHERE (auto_paused_at IS NULL);
-- Create index "device_integration_syncs_schedule_id_key" to table: "device_integration_syncs"
CREATE UNIQUE INDEX "device_integration_syncs_schedule_id_key" ON "device_integration_syncs" ("device_integration_schedule_id");
-- Create "mdm_devices" table
CREATE TABLE "mdm_devices" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "device_integration_config_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "external_id" text NOT NULL,
  "serial_number" text NULL,
  "hostname" text NULL,
  "os_name" text NULL,
  "os_version" text NULL,
  "user_email" text NULL,
  "user_id" text NULL,
  "mdm_last_check_in_at" timestamptz NULL,
  "raw" jsonb NOT NULL DEFAULT '{}',
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "missing_since" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "mdm_devices_organization_id_device_integration_config_id_fkey" FOREIGN KEY ("organization_id", "device_integration_config_id") REFERENCES "device_integration_configs" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mdm_devices_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mdm_devices_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "mdm_devices_config_id_external_id_key" to table: "mdm_devices"
CREATE UNIQUE INDEX "mdm_devices_config_id_external_id_key" ON "mdm_devices" ("device_integration_config_id", "external_id");
-- Create index "mdm_devices_organization_id_lower_user_email_idx" to table: "mdm_devices"
CREATE INDEX "mdm_devices_organization_id_lower_user_email_idx" ON "mdm_devices" ("organization_id", (lower(user_email))) WHERE (missing_since IS NULL);
-- Create index "mdm_devices_organization_id_user_id_idx" to table: "mdm_devices"
CREATE INDEX "mdm_devices_organization_id_user_id_idx" ON "mdm_devices" ("organization_id", "user_id");
-- Create index "mdm_devices_user_id_idx" to table: "mdm_devices"
CREATE INDEX "mdm_devices_user_id_idx" ON "mdm_devices" ("user_id") WHERE (user_id IS NOT NULL);

-- Modify "device_agent_syncs" table
ALTER TABLE "device_agent_syncs" ADD COLUMN "served_etag" text NULL, ADD COLUMN "served_etag_changed_at" timestamptz NULL;
-- Set comment to column: "served_etag" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."served_etag" IS '@access: confidential';
-- Set comment to column: "served_etag_changed_at" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."served_etag_changed_at" IS '@access: confidential';
-- Create "device_agent_state_reports" table
CREATE TABLE "device_agent_state_reports" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "email" text NOT NULL,
  "serial_number" text NULL,
  "hostname" text NULL,
  "schema_version" integer NOT NULL,
  "generated_at" timestamptz NOT NULL,
  "reported_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "applied_etag" text NULL,
  "applied_at" timestamptz NULL,
  "enforcement_halted" boolean NOT NULL,
  "remote_config_etag" text NULL,
  "remote_config_failed" boolean NOT NULL DEFAULT false,
  "withheld_plugins" jsonb NOT NULL DEFAULT '[]',
  "tools" jsonb NOT NULL DEFAULT '[]',
  "degraded_tool_count" integer NOT NULL DEFAULT 0,
  "drift_counting_since" timestamptz NULL,
  "speakeasyd_version" text NULL,
  "speakeasy_version" text NULL,
  "speakeasy_ui_version" text NULL,
  "os" text NULL,
  "arch" text NULL,
  "auto_update" text NULL,
  "update_channel" text NULL,
  "pinned_target" text NULL,
  "current_blocked" boolean NOT NULL DEFAULT false,
  "last_rollback_at" timestamptz NULL,
  "last_rollback_from" text NULL,
  "last_rollback_to" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "device_agent_state_reports_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "device_agent_state_reports_org_lower_serial_lower_email_key" to table: "device_agent_state_reports"
CREATE UNIQUE INDEX "device_agent_state_reports_org_lower_serial_lower_email_key" ON "device_agent_state_reports" ("organization_id", (lower(serial_number)), (lower(email))) NULLS NOT DISTINCT;
-- Create index "device_agent_state_reports_organization_id_lower_email_idx" to table: "device_agent_state_reports"
CREATE INDEX "device_agent_state_reports_organization_id_lower_email_idx" ON "device_agent_state_reports" ("organization_id", (lower(email)));
-- Set comment to column: "id" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."organization_id" IS '@access: confidential';
-- Set comment to column: "email" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."email" IS '@access: confidential-pii';
-- Set comment to column: "serial_number" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."serial_number" IS '@access: confidential-pii';
-- Set comment to column: "hostname" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."hostname" IS '@access: confidential-pii';
-- Set comment to column: "schema_version" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."schema_version" IS '@access: confidential';
-- Set comment to column: "generated_at" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."generated_at" IS '@access: confidential';
-- Set comment to column: "reported_at" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."reported_at" IS '@access: confidential';
-- Set comment to column: "applied_etag" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."applied_etag" IS '@access: confidential';
-- Set comment to column: "applied_at" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."applied_at" IS '@access: confidential';
-- Set comment to column: "enforcement_halted" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."enforcement_halted" IS '@access: confidential';
-- Set comment to column: "remote_config_etag" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."remote_config_etag" IS '@access: confidential';
-- Set comment to column: "remote_config_failed" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."remote_config_failed" IS '@access: confidential';
-- Set comment to column: "withheld_plugins" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."withheld_plugins" IS 'Marketplace and plugin slugs chosen by the organization.
@access: opaque-confidential';
-- Set comment to column: "tools" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."tools" IS 'Speakeasy-defined tool ids and enforcement enums.
@access: confidential';
-- Set comment to column: "degraded_tool_count" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."degraded_tool_count" IS '@access: confidential';
-- Set comment to column: "drift_counting_since" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."drift_counting_since" IS '@access: confidential';
-- Set comment to column: "speakeasyd_version" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."speakeasyd_version" IS '@access: confidential';
-- Set comment to column: "speakeasy_version" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."speakeasy_version" IS '@access: confidential';
-- Set comment to column: "speakeasy_ui_version" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."speakeasy_ui_version" IS '@access: confidential';
-- Set comment to column: "os" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."os" IS '@access: confidential';
-- Set comment to column: "arch" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."arch" IS '@access: confidential';
-- Set comment to column: "auto_update" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."auto_update" IS '@access: confidential';
-- Set comment to column: "update_channel" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."update_channel" IS '@access: confidential';
-- Set comment to column: "pinned_target" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."pinned_target" IS '@access: confidential';
-- Set comment to column: "current_blocked" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."current_blocked" IS '@access: confidential';
-- Set comment to column: "last_rollback_at" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."last_rollback_at" IS '@access: confidential';
-- Set comment to column: "last_rollback_from" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."last_rollback_from" IS '@access: confidential';
-- Set comment to column: "last_rollback_to" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."last_rollback_to" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."updated_at" IS '@access: confidential';

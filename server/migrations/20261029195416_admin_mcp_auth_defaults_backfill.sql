-- Create "admin_mcp_auth_defaults_backfill_runs" table
CREATE TABLE "admin_mcp_auth_defaults_backfill_runs" (
  "id" uuid NOT NULL,
  "staff_subject" text NOT NULL,
  "status" text NOT NULL,
  "version" bigint NOT NULL DEFAULT 1,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id")
);
-- Set comment to column: "id" on table: "admin_mcp_auth_defaults_backfill_runs"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_runs"."id" IS '@access: confidential';
-- Set comment to column: "staff_subject" on table: "admin_mcp_auth_defaults_backfill_runs"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_runs"."staff_subject" IS '@access: confidential-pii';
-- Set comment to column: "status" on table: "admin_mcp_auth_defaults_backfill_runs"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_runs"."status" IS '@access: confidential';
-- Set comment to column: "version" on table: "admin_mcp_auth_defaults_backfill_runs"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_runs"."version" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "admin_mcp_auth_defaults_backfill_runs"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_runs"."expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "admin_mcp_auth_defaults_backfill_runs"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_runs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "admin_mcp_auth_defaults_backfill_runs"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_runs"."updated_at" IS '@access: confidential';
-- Create "admin_mcp_auth_defaults_backfill_targets" table
CREATE TABLE "admin_mcp_auth_defaults_backfill_targets" (
  "run_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "fingerprint" text NOT NULL,
  "outcome" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("run_id", "organization_id"),
  CONSTRAINT "admin_mcp_auth_defaults_backfill_targets_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "admin_mcp_auth_defaults_backfill_targets_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "admin_mcp_auth_defaults_backfill_runs" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "admin_mcp_auth_defaults_backfill_targets_organization_idx" to table: "admin_mcp_auth_defaults_backfill_targets"
CREATE INDEX "admin_mcp_auth_defaults_backfill_targets_organization_idx" ON "admin_mcp_auth_defaults_backfill_targets" ("organization_id");
-- Set comment to column: "run_id" on table: "admin_mcp_auth_defaults_backfill_targets"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_targets"."run_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "admin_mcp_auth_defaults_backfill_targets"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_targets"."organization_id" IS '@access: confidential';
-- Set comment to column: "fingerprint" on table: "admin_mcp_auth_defaults_backfill_targets"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_targets"."fingerprint" IS '@access: confidential';
-- Set comment to column: "outcome" on table: "admin_mcp_auth_defaults_backfill_targets"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_targets"."outcome" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "admin_mcp_auth_defaults_backfill_targets"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_targets"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "admin_mcp_auth_defaults_backfill_targets"
COMMENT ON COLUMN "admin_mcp_auth_defaults_backfill_targets"."updated_at" IS '@access: confidential';

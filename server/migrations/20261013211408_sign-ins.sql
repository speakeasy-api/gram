-- atlas:txmode none

-- Create "sign_in_revocation_cutoffs" table
CREATE TABLE "sign_in_revocation_cutoffs" (
  "organization_id" text NOT NULL,
  "user_id" text NOT NULL,
  "cutoff_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "user_id"),
  CONSTRAINT "sign_in_revocation_cutoffs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to column: "organization_id" on table: "sign_in_revocation_cutoffs"
COMMENT ON COLUMN "sign_in_revocation_cutoffs"."organization_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "sign_in_revocation_cutoffs"
COMMENT ON COLUMN "sign_in_revocation_cutoffs"."user_id" IS '@access: confidential';
-- Set comment to column: "cutoff_at" on table: "sign_in_revocation_cutoffs"
COMMENT ON COLUMN "sign_in_revocation_cutoffs"."cutoff_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "sign_in_revocation_cutoffs"
COMMENT ON COLUMN "sign_in_revocation_cutoffs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "sign_in_revocation_cutoffs"
COMMENT ON COLUMN "sign_in_revocation_cutoffs"."updated_at" IS '@access: confidential';
-- Create "sign_in_revocation_deliveries" table
CREATE TABLE "sign_in_revocation_deliveries" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "kind" text NOT NULL,
  "target" text NOT NULL,
  "effective_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "attempts" integer NOT NULL DEFAULT 0,
  "last_error" text NULL,
  "delivered_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "sign_in_revocation_deliveries_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "sign_in_revocation_deliveries_organization_id_idx" to table: "sign_in_revocation_deliveries"
CREATE INDEX "sign_in_revocation_deliveries_organization_id_idx" ON "sign_in_revocation_deliveries" ("organization_id");
-- Create index "sign_in_revocation_deliveries_pending_idx" to table: "sign_in_revocation_deliveries"
CREATE INDEX "sign_in_revocation_deliveries_pending_idx" ON "sign_in_revocation_deliveries" ("created_at") WHERE (delivered_at IS NULL);
-- Create index "sign_in_revocation_deliveries_pending_key" to table: "sign_in_revocation_deliveries"
CREATE UNIQUE INDEX "sign_in_revocation_deliveries_pending_key" ON "sign_in_revocation_deliveries" ("organization_id", "kind", "target") WHERE (delivered_at IS NULL);
-- Set comment to column: "id" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."organization_id" IS '@access: confidential';
-- Set comment to column: "kind" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."kind" IS '@access: confidential';
-- Set comment to column: "target" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."target" IS '@access: secret-restricted';
-- Set comment to column: "effective_at" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."effective_at" IS '@access: confidential';
-- Set comment to column: "attempts" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."attempts" IS '@access: confidential';
-- Set comment to column: "last_error" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."last_error" IS '@access: opaque-restricted';
-- Set comment to column: "delivered_at" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."delivered_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."updated_at" IS '@access: confidential';
-- Create "sign_ins" table
CREATE TABLE "sign_ins" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "user_id" text NOT NULL,
  "workos_session_id" text NOT NULL,
  "authentication_method" text NOT NULL,
  "authenticated_at" timestamptz NOT NULL,
  "revoked_at" timestamptz NULL,
  "revoke_reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "sign_ins_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "sign_ins_organization_id_id_key" to table: "sign_ins"
CREATE UNIQUE INDEX "sign_ins_organization_id_id_key" ON "sign_ins" ("organization_id", "id");
-- Create index "sign_ins_organization_id_user_id_idx" to table: "sign_ins"
CREATE INDEX "sign_ins_organization_id_user_id_idx" ON "sign_ins" ("organization_id", "user_id");
-- Create index "sign_ins_organization_id_workos_session_id_key" to table: "sign_ins"
CREATE UNIQUE INDEX "sign_ins_organization_id_workos_session_id_key" ON "sign_ins" ("organization_id", "workos_session_id");
-- Create index "sign_ins_user_id_workos_session_id_idx" to table: "sign_ins"
CREATE INDEX "sign_ins_user_id_workos_session_id_idx" ON "sign_ins" ("user_id", "workos_session_id");
-- Set comment to column: "id" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."organization_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."user_id" IS '@access: confidential';
-- Set comment to column: "workos_session_id" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."workos_session_id" IS '@access: restricted';
-- Set comment to column: "authentication_method" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."authentication_method" IS '@access: confidential';
-- Set comment to column: "authenticated_at" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."authenticated_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."revoked_at" IS '@access: confidential';
-- Set comment to column: "revoke_reason" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."revoke_reason" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."updated_at" IS '@access: confidential';
-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "sign_in_id" uuid NULL, ADD CONSTRAINT "user_sessions_sign_in_tenant_fkey" FOREIGN KEY ("organization_id", "sign_in_id") REFERENCES "sign_ins" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("sign_in_id") NOT VALID;
ALTER TABLE "user_sessions" VALIDATE CONSTRAINT "user_sessions_sign_in_tenant_fkey";
-- Create index "user_sessions_sign_in_id_idx" to table: "user_sessions"
CREATE INDEX CONCURRENTLY "user_sessions_sign_in_id_idx" ON "user_sessions" ("organization_id", "sign_in_id") WHERE (sign_in_id IS NOT NULL);
-- Set comment to column: "sign_in_id" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."sign_in_id" IS '@access: confidential';

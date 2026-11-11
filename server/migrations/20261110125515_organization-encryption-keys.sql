-- Modify "external_keys" table
ALTER TABLE "external_keys" ADD COLUMN "purpose" text NOT NULL DEFAULT 'sign';
-- Set comment to column: "purpose" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."purpose" IS '@access: confidential';
-- Create "organization_encryption_configs" table
CREATE TABLE "organization_encryption_configs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "mode" text NOT NULL,
  "state" text NOT NULL,
  "kek_external_key_id" uuid NOT NULL,
  "previous_kek_external_key_id" uuid NULL,
  "switch_started_at" timestamptz NULL,
  "switch_completed_at" timestamptz NULL,
  "suspended_at" timestamptz NULL,
  "suspended_reason" text NULL,
  "last_kek_check_at" timestamptz NULL,
  "last_kek_check_error" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_encryption_configs_kek_tenant_fkey" FOREIGN KEY ("organization_id", "kek_external_key_id") REFERENCES "external_keys" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "organization_encryption_configs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_encryption_configs_previous_kek_tenant_fkey" FOREIGN KEY ("organization_id", "previous_kek_external_key_id") REFERENCES "external_keys" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "organization_encryption_configs_kek_tenant_idx" to table: "organization_encryption_configs"
CREATE INDEX "organization_encryption_configs_kek_tenant_idx" ON "organization_encryption_configs" ("organization_id", "kek_external_key_id");
-- Create index "organization_encryption_configs_organization_id_id_key" to table: "organization_encryption_configs"
CREATE UNIQUE INDEX "organization_encryption_configs_organization_id_id_key" ON "organization_encryption_configs" ("organization_id", "id");
-- Create index "organization_encryption_configs_organization_id_key" to table: "organization_encryption_configs"
CREATE UNIQUE INDEX "organization_encryption_configs_organization_id_key" ON "organization_encryption_configs" ("organization_id") WHERE (deleted IS FALSE);
-- Create index "organization_encryption_configs_previous_kek_tenant_idx" to table: "organization_encryption_configs"
CREATE INDEX "organization_encryption_configs_previous_kek_tenant_idx" ON "organization_encryption_configs" ("organization_id", "previous_kek_external_key_id") WHERE (previous_kek_external_key_id IS NOT NULL);
-- Set comment to column: "id" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."organization_id" IS '@access: confidential';
-- Set comment to column: "mode" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."mode" IS '@access: confidential';
-- Set comment to column: "state" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."state" IS '@access: confidential';
-- Set comment to column: "kek_external_key_id" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."kek_external_key_id" IS '@access: confidential';
-- Set comment to column: "previous_kek_external_key_id" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."previous_kek_external_key_id" IS '@access: confidential';
-- Set comment to column: "switch_started_at" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."switch_started_at" IS '@access: confidential';
-- Set comment to column: "switch_completed_at" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."switch_completed_at" IS '@access: confidential';
-- Set comment to column: "suspended_at" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."suspended_at" IS '@access: confidential';
-- Set comment to column: "suspended_reason" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."suspended_reason" IS 'Provider error summary; never carries key material or ciphertext.
@access: restricted';
-- Set comment to column: "last_kek_check_at" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."last_kek_check_at" IS '@access: confidential';
-- Set comment to column: "last_kek_check_error" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."last_kek_check_error" IS 'Provider error summary; never carries key material or ciphertext.
@access: restricted';
-- Set comment to column: "created_at" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "organization_encryption_configs"
COMMENT ON COLUMN "organization_encryption_configs"."deleted" IS '@access: confidential';
-- Create "data_encryption_keys" table
CREATE TABLE "data_encryption_keys" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "encryption_config_id" uuid NOT NULL,
  "use_case" text NOT NULL,
  "state" text NOT NULL,
  "activated_at" timestamptz NULL,
  "retired_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "data_encryption_keys_config_tenant_fkey" FOREIGN KEY ("organization_id", "encryption_config_id") REFERENCES "organization_encryption_configs" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "data_encryption_keys_use_case_check" CHECK (use_case <> ''::text)
);
-- Create index "data_encryption_keys_config_tenant_idx" to table: "data_encryption_keys"
CREATE INDEX "data_encryption_keys_config_tenant_idx" ON "data_encryption_keys" ("organization_id", "encryption_config_id");
-- Create index "data_encryption_keys_organization_id_id_key" to table: "data_encryption_keys"
CREATE UNIQUE INDEX "data_encryption_keys_organization_id_id_key" ON "data_encryption_keys" ("organization_id", "id");
-- Create index "data_encryption_keys_organization_id_use_case_active_key" to table: "data_encryption_keys"
CREATE UNIQUE INDEX "data_encryption_keys_organization_id_use_case_active_key" ON "data_encryption_keys" ("organization_id", "use_case") WHERE ((state = 'active'::text) AND (deleted IS FALSE));
-- Set comment to column: "id" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."organization_id" IS '@access: confidential';
-- Set comment to column: "encryption_config_id" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."encryption_config_id" IS '@access: confidential';
-- Set comment to column: "use_case" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."use_case" IS '@access: confidential';
-- Set comment to column: "state" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."state" IS '@access: confidential';
-- Set comment to column: "activated_at" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."activated_at" IS '@access: confidential';
-- Set comment to column: "retired_at" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."retired_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "data_encryption_keys"
COMMENT ON COLUMN "data_encryption_keys"."deleted" IS '@access: confidential';
-- Create "data_encryption_key_wraps" table
CREATE TABLE "data_encryption_key_wraps" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "data_encryption_key_id" uuid NOT NULL,
  "external_key_id" uuid NOT NULL,
  "external_key_version" text NULL,
  "wrapped_key" bytea NOT NULL,
  "last_unwrapped_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "data_encryption_key_wraps_dek_tenant_fkey" FOREIGN KEY ("organization_id", "data_encryption_key_id") REFERENCES "data_encryption_keys" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "data_encryption_key_wraps_external_key_tenant_fkey" FOREIGN KEY ("organization_id", "external_key_id") REFERENCES "external_keys" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "data_encryption_key_wraps_wrapped_key_check" CHECK (octet_length(wrapped_key) > 0)
);
-- Create index "data_encryption_key_wraps_dek_external_key_key" to table: "data_encryption_key_wraps"
CREATE UNIQUE INDEX "data_encryption_key_wraps_dek_external_key_key" ON "data_encryption_key_wraps" ("data_encryption_key_id", "external_key_id") WHERE (deleted IS FALSE);
-- Create index "data_encryption_key_wraps_dek_tenant_idx" to table: "data_encryption_key_wraps"
CREATE INDEX "data_encryption_key_wraps_dek_tenant_idx" ON "data_encryption_key_wraps" ("organization_id", "data_encryption_key_id");
-- Create index "data_encryption_key_wraps_external_key_tenant_idx" to table: "data_encryption_key_wraps"
CREATE INDEX "data_encryption_key_wraps_external_key_tenant_idx" ON "data_encryption_key_wraps" ("organization_id", "external_key_id");
-- Set comment to column: "id" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."organization_id" IS '@access: confidential';
-- Set comment to column: "data_encryption_key_id" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."data_encryption_key_id" IS '@access: confidential';
-- Set comment to column: "external_key_id" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."external_key_id" IS '@access: confidential';
-- Set comment to column: "external_key_version" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."external_key_version" IS '@access: restricted';
-- Set comment to column: "wrapped_key" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."wrapped_key" IS '@access: secret-restricted';
-- Set comment to column: "last_unwrapped_at" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."last_unwrapped_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."deleted" IS '@access: confidential';
-- Create "managed_encryption_keys" table
CREATE TABLE "managed_encryption_keys" (
  "external_key_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "kms_disabled_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("external_key_id"),
  CONSTRAINT "managed_encryption_keys_external_key_tenant_fkey" FOREIGN KEY ("organization_id", "external_key_id") REFERENCES "external_keys" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "managed_encryption_keys_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "managed_encryption_keys_organization_id_external_key_id_idx" to table: "managed_encryption_keys"
CREATE INDEX "managed_encryption_keys_organization_id_external_key_id_idx" ON "managed_encryption_keys" ("organization_id", "external_key_id");
-- Set comment to column: "external_key_id" on table: "managed_encryption_keys"
COMMENT ON COLUMN "managed_encryption_keys"."external_key_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "managed_encryption_keys"
COMMENT ON COLUMN "managed_encryption_keys"."organization_id" IS '@access: confidential';
-- Set comment to column: "kms_disabled_at" on table: "managed_encryption_keys"
COMMENT ON COLUMN "managed_encryption_keys"."kms_disabled_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "managed_encryption_keys"
COMMENT ON COLUMN "managed_encryption_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "managed_encryption_keys"
COMMENT ON COLUMN "managed_encryption_keys"."updated_at" IS '@access: confidential';

-- Create "organization_encryption_transitions" table
CREATE TABLE "organization_encryption_transitions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "use_case" text NOT NULL,
  "state" text NOT NULL,
  "generation" bigint NOT NULL DEFAULT 1,
  "state_changed_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "platform_copies_cleared_at" timestamptz NULL,
  "last_backfill_started_at" timestamptz NULL,
  "last_backfill_completed_at" timestamptz NULL,
  "last_backfill_converted_count" bigint NULL,
  "last_backfill_remaining_count" bigint NULL,
  "last_backfill_unreadable_count" bigint NULL,
  "last_backfill_error" text NULL,
  "last_verified_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_encryption_transitions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_encryption_transitions_use_case_check" CHECK (use_case <> ''::text)
);
-- Create index "organization_encryption_transitions_org_use_case_key" to table: "organization_encryption_transitions"
CREATE UNIQUE INDEX "organization_encryption_transitions_org_use_case_key" ON "organization_encryption_transitions" ("organization_id", "use_case");
-- Set comment to column: "id" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."organization_id" IS '@access: confidential';
-- Set comment to column: "use_case" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."use_case" IS '@access: confidential';
-- Set comment to column: "state" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."state" IS '@access: confidential';
-- Set comment to column: "generation" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."generation" IS 'Starts at 1 and increases on every state change; never decreases.
@access: confidential';
-- Set comment to column: "state_changed_at" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."state_changed_at" IS '@access: confidential';
-- Set comment to column: "platform_copies_cleared_at" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."platform_copies_cleared_at" IS 'When finalize finished destroying the platform-key copies. Set only in org_kms.
@access: confidential';
-- Set comment to column: "last_backfill_started_at" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."last_backfill_started_at" IS '@access: confidential';
-- Set comment to column: "last_backfill_completed_at" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."last_backfill_completed_at" IS '@access: confidential';
-- Set comment to column: "last_backfill_converted_count" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."last_backfill_converted_count" IS '@access: confidential';
-- Set comment to column: "last_backfill_remaining_count" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."last_backfill_remaining_count" IS '@access: confidential';
-- Set comment to column: "last_backfill_unreadable_count" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."last_backfill_unreadable_count" IS '@access: confidential';
-- Set comment to column: "last_backfill_error" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."last_backfill_error" IS 'Short error summary; never carries values, ciphertext or key material.
@access: restricted';
-- Set comment to column: "last_verified_at" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."last_verified_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_encryption_transitions"
COMMENT ON COLUMN "organization_encryption_transitions"."updated_at" IS '@access: confidential';
-- Create "organization_encryption_unreadable_values" table
CREATE TABLE "organization_encryption_unreadable_values" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "use_case" text NOT NULL,
  "source_column" text NOT NULL,
  "row_id" text NOT NULL,
  "platform_ciphertext_sha256" bytea NOT NULL,
  "error_code" text NOT NULL,
  "first_detected_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_detected_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_encryption_unreadable_values_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "organization_encryption_unreadable_values_source_row_key" to table: "organization_encryption_unreadable_values"
CREATE UNIQUE INDEX "organization_encryption_unreadable_values_source_row_key" ON "organization_encryption_unreadable_values" ("organization_id", "use_case", "source_column", "row_id");
-- Set comment to column: "id" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."organization_id" IS '@access: confidential';
-- Set comment to column: "use_case" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."use_case" IS '@access: confidential';
-- Set comment to column: "source_column" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."source_column" IS 'Logical table.column name registered by Speakeasy code.
@access: confidential';
-- Set comment to column: "row_id" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."row_id" IS 'Single-column primary key of the source row, as produced by <pk>::text.
@access: confidential';
-- Set comment to column: "platform_ciphertext_sha256" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."platform_ciphertext_sha256" IS 'SHA-256 of the UTF-8 bytes of the stored platform-key ciphertext text that failed to decrypt.
@access: secret-restricted';
-- Set comment to column: "error_code" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."error_code" IS 'Speakeasy decryption error code; never the value or ciphertext.
@access: confidential';
-- Set comment to column: "first_detected_at" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."first_detected_at" IS '@access: confidential';
-- Set comment to column: "last_detected_at" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."last_detected_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_encryption_unreadable_values"
COMMENT ON COLUMN "organization_encryption_unreadable_values"."updated_at" IS '@access: confidential';

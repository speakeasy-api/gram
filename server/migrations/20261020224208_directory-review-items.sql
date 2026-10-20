-- Create "directory_review_attribute_opt_outs" table
CREATE TABLE "directory_review_attribute_opt_outs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "attribute_key" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "directory_review_attribute_opt_outs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "directory_review_attribute_opt_outs_attribute_key_check" CHECK (attribute_key <> ''::text)
);
-- Create index "directory_review_attribute_opt_outs_org_key_key" to table: "directory_review_attribute_opt_outs"
CREATE UNIQUE INDEX "directory_review_attribute_opt_outs_org_key_key" ON "directory_review_attribute_opt_outs" ("organization_id", "attribute_key") WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "directory_review_attribute_opt_outs"
COMMENT ON COLUMN "directory_review_attribute_opt_outs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "directory_review_attribute_opt_outs"
COMMENT ON COLUMN "directory_review_attribute_opt_outs"."organization_id" IS '@access: confidential';
-- Set comment to column: "attribute_key" on table: "directory_review_attribute_opt_outs"
COMMENT ON COLUMN "directory_review_attribute_opt_outs"."attribute_key" IS '@access: opaque-confidential';
-- Set comment to column: "created_at" on table: "directory_review_attribute_opt_outs"
COMMENT ON COLUMN "directory_review_attribute_opt_outs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "directory_review_attribute_opt_outs"
COMMENT ON COLUMN "directory_review_attribute_opt_outs"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "directory_review_attribute_opt_outs"
COMMENT ON COLUMN "directory_review_attribute_opt_outs"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "directory_review_attribute_opt_outs"
COMMENT ON COLUMN "directory_review_attribute_opt_outs"."deleted" IS '@access: confidential';
-- Create "directory_review_items" table
CREATE TABLE "directory_review_items" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "kind" text NOT NULL,
  "directory_group_id" uuid NULL,
  "attribute_key" text NULL,
  "attribute_value" text NULL,
  "occurrence" integer NOT NULL,
  "state" text NOT NULL,
  "resolved_at" timestamptz NULL,
  "resolved_by_user_id" text NULL,
  "notified_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "directory_review_items_directory_group_fkey" FOREIGN KEY ("organization_id", "directory_group_id") REFERENCES "directory_groups" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "directory_review_items_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "directory_review_items_source_columns_check" CHECK (((attribute_key IS NULL) = (attribute_value IS NULL)) AND (NOT ((directory_group_id IS NOT NULL) AND (attribute_key IS NOT NULL))))
);
-- Create index "directory_review_items_org_attribute_occurrence_key" to table: "directory_review_items"
CREATE UNIQUE INDEX "directory_review_items_org_attribute_occurrence_key" ON "directory_review_items" ("organization_id", "kind", "attribute_key", "attribute_value", "occurrence") WHERE (attribute_key IS NOT NULL);
-- Create index "directory_review_items_org_attribute_pending_key" to table: "directory_review_items"
CREATE UNIQUE INDEX "directory_review_items_org_attribute_pending_key" ON "directory_review_items" ("organization_id", "kind", "attribute_key", "attribute_value") WHERE ((state = 'pending'::text) AND (attribute_key IS NOT NULL));
-- Create index "directory_review_items_org_group_all_idx" to table: "directory_review_items"
CREATE INDEX "directory_review_items_org_group_all_idx" ON "directory_review_items" ("organization_id", "directory_group_id");
-- Create index "directory_review_items_org_group_occurrence_key" to table: "directory_review_items"
CREATE UNIQUE INDEX "directory_review_items_org_group_occurrence_key" ON "directory_review_items" ("organization_id", "kind", "directory_group_id", "occurrence") WHERE (directory_group_id IS NOT NULL);
-- Create index "directory_review_items_org_group_pending_key" to table: "directory_review_items"
CREATE UNIQUE INDEX "directory_review_items_org_group_pending_key" ON "directory_review_items" ("organization_id", "kind", "directory_group_id") WHERE ((state = 'pending'::text) AND (directory_group_id IS NOT NULL));
-- Create index "directory_review_items_org_pending_idx" to table: "directory_review_items"
CREATE INDEX "directory_review_items_org_pending_idx" ON "directory_review_items" ("organization_id", "created_at") WHERE (state = 'pending'::text);
-- Set comment to column: "id" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."organization_id" IS '@access: confidential';
-- Set comment to column: "kind" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."kind" IS '@access: confidential';
-- Set comment to column: "directory_group_id" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."directory_group_id" IS '@access: confidential';
-- Set comment to column: "attribute_key" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."attribute_key" IS '@access: opaque-confidential';
-- Set comment to column: "attribute_value" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."attribute_value" IS '@access: opaque-restricted';
-- Set comment to column: "occurrence" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."occurrence" IS '@access: confidential';
-- Set comment to column: "state" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."state" IS '@access: confidential';
-- Set comment to column: "resolved_at" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."resolved_at" IS '@access: confidential';
-- Set comment to column: "resolved_by_user_id" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."resolved_by_user_id" IS '@access: confidential';
-- Set comment to column: "notified_at" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."notified_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "directory_review_items"
COMMENT ON COLUMN "directory_review_items"."updated_at" IS '@access: confidential';
-- Create "directory_review_org_states" table
CREATE TABLE "directory_review_org_states" (
  "organization_id" text NOT NULL,
  "baseline_at" timestamptz NOT NULL,
  "emails_disabled_at" timestamptz NULL,
  "last_emailed_at" timestamptz NULL,
  "reminders_sent" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "directory_review_org_states_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to column: "organization_id" on table: "directory_review_org_states"
COMMENT ON COLUMN "directory_review_org_states"."organization_id" IS '@access: confidential';
-- Set comment to column: "baseline_at" on table: "directory_review_org_states"
COMMENT ON COLUMN "directory_review_org_states"."baseline_at" IS '@access: confidential';
-- Set comment to column: "emails_disabled_at" on table: "directory_review_org_states"
COMMENT ON COLUMN "directory_review_org_states"."emails_disabled_at" IS '@access: confidential';
-- Set comment to column: "last_emailed_at" on table: "directory_review_org_states"
COMMENT ON COLUMN "directory_review_org_states"."last_emailed_at" IS '@access: confidential';
-- Set comment to column: "reminders_sent" on table: "directory_review_org_states"
COMMENT ON COLUMN "directory_review_org_states"."reminders_sent" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "directory_review_org_states"
COMMENT ON COLUMN "directory_review_org_states"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "directory_review_org_states"
COMMENT ON COLUMN "directory_review_org_states"."updated_at" IS '@access: confidential';

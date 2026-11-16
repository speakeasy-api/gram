-- Create "user_session_consent_resources" table
CREATE TABLE "user_session_consent_resources" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "user_session_consent_id" uuid NULL,
  "resource" text NOT NULL,
  "inventory_hash" text NOT NULL,
  "remote_set_hash" text NOT NULL,
  "tool_selection" jsonb NULL,
  "session_duration_hours" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_session_consent_resources_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "user_session_consent_resources_user_session_consent_id_fkey" FOREIGN KEY ("user_session_consent_id") REFERENCES "user_session_consents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "user_session_consent_resources_consent_resource_key" to table: "user_session_consent_resources"
CREATE UNIQUE INDEX "user_session_consent_resources_consent_resource_key" ON "user_session_consent_resources" ("user_session_consent_id", "resource") WHERE (deleted IS FALSE);
-- Create index "user_session_consent_resources_project_id_idx" to table: "user_session_consent_resources"
CREATE INDEX "user_session_consent_resources_project_id_idx" ON "user_session_consent_resources" ("project_id");
-- Set comment to column: "id" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."project_id" IS 'Target resource project, independent of issuer tenancy.
@access: confidential';
-- Set comment to column: "user_session_consent_id" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."user_session_consent_id" IS '@access: confidential';
-- Set comment to column: "resource" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."resource" IS '@access: confidential';
-- Set comment to column: "inventory_hash" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."inventory_hash" IS 'Versioned fingerprint of the complete approved tool inventory.
@access: confidential';
-- Set comment to column: "remote_set_hash" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."remote_set_hash" IS 'Fingerprint of the approved upstream clients, resources and requested permissions.
@access: confidential';
-- Set comment to column: "tool_selection" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."tool_selection" IS '@access: opaque-restricted';
-- Set comment to column: "session_duration_hours" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."session_duration_hours" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "user_session_consent_resources"
COMMENT ON COLUMN "user_session_consent_resources"."deleted" IS '@access: confidential';

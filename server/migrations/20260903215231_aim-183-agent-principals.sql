-- Create "agents" table
CREATE TABLE "agents" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "owner_user_id" text NOT NULL,
  "name" text NOT NULL,
  "suspended_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "owner_reassignment_required_at" timestamptz NULL,
  "owner_reassignment_reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "agents_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agents_owner_tenant_fkey" FOREIGN KEY ("organization_id", "owner_user_id") REFERENCES "organization_user_relationships" ("organization_id", "user_id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "agents_lifecycle_state_check" CHECK ((revoked_at IS NULL) OR (suspended_at IS NULL)),
  CONSTRAINT "agents_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 120)),
  CONSTRAINT "agents_owner_reassignment_state_check" CHECK ((owner_reassignment_required_at IS NULL) = (owner_reassignment_reason IS NULL))
);
-- Create index "agents_organization_id_id_key" to table: "agents"
CREATE UNIQUE INDEX "agents_organization_id_id_key" ON "agents" ("organization_id", "id");
-- Create index "agents_organization_name_key" to table: "agents"
CREATE UNIQUE INDEX "agents_organization_name_key" ON "agents" ("organization_id", (lower(name))) WHERE (deleted IS FALSE);
-- Create index "agents_organization_owner_idx" to table: "agents"
CREATE INDEX "agents_organization_owner_idx" ON "agents" ("organization_id", "owner_user_id") WHERE (deleted IS FALSE);
-- Create index "agents_organization_owner_all_idx" to table: "agents"
CREATE INDEX "agents_organization_owner_all_idx" ON "agents" ("organization_id", "owner_user_id");

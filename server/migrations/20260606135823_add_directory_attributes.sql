-- Create "workos_directory_attributes_syncs" table
CREATE TABLE "workos_directory_attributes_syncs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "entity_id" text NOT NULL,
  "entity_type" text NOT NULL,
  "last_event_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id")
);
-- Create index "workos_directory_attributes_syncs_entity_key" to table: "workos_directory_attributes_syncs"
CREATE UNIQUE INDEX "workos_directory_attributes_syncs_entity_key" ON "workos_directory_attributes_syncs" ("entity_type", "entity_id");
-- Create "groups" table
CREATE TABLE "groups" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "workos_directory_group_id" text NOT NULL,
  "name" text NOT NULL,
  "attributes" jsonb NOT NULL DEFAULT '{}',
  "attributes_content_hash" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "groups_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "groups_organization_id_idx" to table: "groups"
CREATE INDEX "groups_organization_id_idx" ON "groups" ("organization_id");
-- Create index "groups_workos_directory_group_id_key" to table: "groups"
CREATE UNIQUE INDEX "groups_workos_directory_group_id_key" ON "groups" ("workos_directory_group_id");
-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "attributes" jsonb NOT NULL DEFAULT '{}', ADD COLUMN "attributes_content_hash" text NULL;
-- Create "user_group_memberships" table
CREATE TABLE "user_group_memberships" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "user_id" text NOT NULL,
  "group_id" uuid NOT NULL,
  "workos_directory_user_id" text NOT NULL,
  "workos_directory_group_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "user_group_memberships_group_id_fkey" FOREIGN KEY ("group_id") REFERENCES "groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_group_memberships_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "user_group_memberships_current_key" to table: "user_group_memberships"
CREATE UNIQUE INDEX "user_group_memberships_current_key" ON "user_group_memberships" ("user_id", "group_id");

-- Create "workos_directory_attributes_syncs" table
CREATE TABLE "workos_directory_attributes_syncs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "workos_organization_id" text NOT NULL,
  "entity_id" text NOT NULL,
  "entity_type" text NOT NULL,
  "last_event_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id")
);
-- Create index "workos_directory_attributes_syncs_entity_key" to table: "workos_directory_attributes_syncs"
CREATE UNIQUE INDEX "workos_directory_attributes_syncs_entity_key" ON "workos_directory_attributes_syncs" ("workos_organization_id", "entity_type", "entity_id");
-- Create "workos_directory_groups" table
CREATE TABLE "workos_directory_groups" (
  "id" text NOT NULL,
  "workos_directory_group_id" text NOT NULL,
  "workos_organization_id" text NOT NULL,
  "name" text NOT NULL,
  "attributes" jsonb NOT NULL DEFAULT '{}',
  "attributes_content_hash" text NULL,
  "workos_created_at" timestamptz NULL,
  "workos_updated_at" timestamptz NULL,
  "workos_deleted_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id")
);
-- Create index "workos_directory_groups_workos_directory_group_id_key" to table: "workos_directory_groups"
CREATE UNIQUE INDEX "workos_directory_groups_workos_directory_group_id_key" ON "workos_directory_groups" ("workos_directory_group_id");
-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "attributes" jsonb NOT NULL DEFAULT '{}', ADD COLUMN "attributes_content_hash" text NULL;
-- Create "workos_directory_user_group_membership" table
CREATE TABLE "workos_directory_user_group_membership" (
  "id" text NOT NULL,
  "user_id" text NOT NULL,
  "group_id" text NOT NULL,
  "workos_directory_user_id" text NOT NULL,
  "workos_directory_group_id" text NOT NULL,
  "joined_at" timestamptz NOT NULL,
  "left_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "workos_directory_user_group_membership_group_id_fkey" FOREIGN KEY ("group_id") REFERENCES "workos_directory_groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workos_directory_user_group_membership_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "workos_directory_user_group_membership_current_key" to table: "workos_directory_user_group_membership"
CREATE UNIQUE INDEX "workos_directory_user_group_membership_current_key" ON "workos_directory_user_group_membership" ("user_id", "group_id") WHERE (left_at IS NULL);
-- Create index "workos_directory_user_group_membership_group_history" to table: "workos_directory_user_group_membership"
CREATE INDEX "workos_directory_user_group_membership_group_history" ON "workos_directory_user_group_membership" ("group_id", "joined_at" DESC);
-- Create index "workos_directory_user_group_membership_user_history" to table: "workos_directory_user_group_membership"
CREATE INDEX "workos_directory_user_group_membership_user_history" ON "workos_directory_user_group_membership" ("user_id", "joined_at" DESC);

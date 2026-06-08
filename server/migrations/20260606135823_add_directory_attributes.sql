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
-- Create "directory_groups" table
CREATE TABLE "directory_groups" (
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
  CONSTRAINT "directory_groups_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "directory_groups_organization_id_idx" to table: "directory_groups"
CREATE INDEX "directory_groups_organization_id_idx" ON "directory_groups" ("organization_id");
-- Create index "directory_groups_workos_directory_group_id_key" to table: "directory_groups"
CREATE UNIQUE INDEX "directory_groups_workos_directory_group_id_key" ON "directory_groups" ("workos_directory_group_id");
-- Create "directory_users" table
CREATE TABLE "directory_users" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "user_id" text NULL,
  "workos_directory_user_id" text NOT NULL,
  "email" text NULL,
  "attributes" jsonb NOT NULL DEFAULT '{}',
  "attributes_content_hash" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "directory_users_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "directory_users_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "directory_users_organization_id_idx" to table: "directory_users"
CREATE INDEX "directory_users_organization_id_idx" ON "directory_users" ("organization_id");
-- Create index "directory_users_user_id_idx" to table: "directory_users"
CREATE INDEX "directory_users_user_id_idx" ON "directory_users" ("user_id") WHERE "user_id" IS NOT NULL;
-- Create index "directory_users_workos_directory_user_id_key" to table: "directory_users"
CREATE UNIQUE INDEX "directory_users_workos_directory_user_id_key" ON "directory_users" ("workos_directory_user_id");
-- Create "directory_user_group_memberships" table
CREATE TABLE "directory_user_group_memberships" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "user_id" text NOT NULL,
  "group_id" uuid NOT NULL,
  "workos_directory_user_id" text NOT NULL,
  "workos_directory_group_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "directory_user_group_memberships_group_id_fkey" FOREIGN KEY ("group_id") REFERENCES "directory_groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "directory_user_group_memberships_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "directory_user_group_memberships_current_key" to table: "directory_user_group_memberships"
CREATE UNIQUE INDEX "directory_user_group_memberships_current_key" ON "directory_user_group_memberships" ("user_id", "group_id");

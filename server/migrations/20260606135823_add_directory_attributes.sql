-- Create "directory_groups" table
CREATE TABLE "directory_groups" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "workos_directory_group_id" text NOT NULL,
  "name" text NOT NULL,
  "attributes" jsonb NOT NULL DEFAULT '{}',
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
  "directory_user_id" uuid NOT NULL,
  "directory_group_id" uuid NOT NULL,
  "workos_directory_user_id" text NOT NULL,
  "workos_directory_group_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "directory_user_group_memberships_directory_group_id_fkey" FOREIGN KEY ("directory_group_id") REFERENCES "directory_groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "directory_user_group_memberships_directory_user_id_fkey" FOREIGN KEY ("directory_user_id") REFERENCES "directory_users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "directory_user_group_memberships_current_key" to table: "directory_user_group_memberships"
CREATE UNIQUE INDEX "directory_user_group_memberships_current_key" ON "directory_user_group_memberships" ("directory_user_id", "directory_group_id") WHERE "deleted" IS FALSE;
-- Create index "directory_user_group_memberships_directory_group_id_idx" to table: "directory_user_group_memberships"
CREATE INDEX "directory_user_group_memberships_directory_group_id_idx" ON "directory_user_group_memberships" ("directory_group_id");

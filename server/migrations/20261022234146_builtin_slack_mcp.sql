-- Create "builtin_mcp_server_members" table
CREATE TABLE "builtin_mcp_server_members" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "meta_mcp_server_id" uuid NULL,
  "builtin_key" text NOT NULL,
  "environment_id" uuid NULL,
  "sort_order" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "builtin_mcp_server_members_environment_id_fkey" FOREIGN KEY ("environment_id") REFERENCES "environments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "builtin_mcp_server_members_meta_mcp_server_id_fkey" FOREIGN KEY ("meta_mcp_server_id") REFERENCES "meta_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "builtin_mcp_server_members_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "builtin_mcp_server_members_builtin_key_check" CHECK (builtin_key <> ''::text)
);
-- Create index "builtin_mcp_server_members_gateway_key_idx" to table: "builtin_mcp_server_members"
CREATE UNIQUE INDEX "builtin_mcp_server_members_gateway_key_idx" ON "builtin_mcp_server_members" ("meta_mcp_server_id", "builtin_key");
-- Create index "builtin_mcp_server_members_project_id_idx" to table: "builtin_mcp_server_members"
CREATE INDEX "builtin_mcp_server_members_project_id_idx" ON "builtin_mcp_server_members" ("project_id");
-- Set comment to column: "id" on table: "builtin_mcp_server_members"
COMMENT ON COLUMN "builtin_mcp_server_members"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "builtin_mcp_server_members"
COMMENT ON COLUMN "builtin_mcp_server_members"."project_id" IS '@access: confidential';
-- Set comment to column: "meta_mcp_server_id" on table: "builtin_mcp_server_members"
COMMENT ON COLUMN "builtin_mcp_server_members"."meta_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "builtin_key" on table: "builtin_mcp_server_members"
COMMENT ON COLUMN "builtin_mcp_server_members"."builtin_key" IS '@access: shared';
-- Set comment to column: "environment_id" on table: "builtin_mcp_server_members"
COMMENT ON COLUMN "builtin_mcp_server_members"."environment_id" IS '@access: confidential';
-- Set comment to column: "sort_order" on table: "builtin_mcp_server_members"
COMMENT ON COLUMN "builtin_mcp_server_members"."sort_order" IS '@access: shared';
-- Set comment to column: "created_at" on table: "builtin_mcp_server_members"
COMMENT ON COLUMN "builtin_mcp_server_members"."created_at" IS '@access: shared';
-- Set comment to column: "updated_at" on table: "builtin_mcp_server_members"
COMMENT ON COLUMN "builtin_mcp_server_members"."updated_at" IS '@access: shared';
-- Create "builtin_mcp_settings" table
CREATE TABLE "builtin_mcp_settings" (
  "project_id" uuid NULL,
  "builtin_key" text NOT NULL,
  "hidden" boolean NOT NULL DEFAULT false,
  "environment_id" uuid NULL,
  "version" bigint NOT NULL DEFAULT 1,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  PRIMARY KEY ("id"),
  CONSTRAINT "builtin_mcp_settings_environment_id_fkey" FOREIGN KEY ("environment_id") REFERENCES "environments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "builtin_mcp_settings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "builtin_mcp_settings_builtin_key_check" CHECK (builtin_key <> ''::text)
);
-- Create index "builtin_mcp_settings_project_key_idx" to table: "builtin_mcp_settings"
CREATE UNIQUE INDEX "builtin_mcp_settings_project_key_idx" ON "builtin_mcp_settings" ("project_id", "builtin_key");
-- Set comment to column: "project_id" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."project_id" IS '@access: confidential';
-- Set comment to column: "builtin_key" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."builtin_key" IS '@access: shared';
-- Set comment to column: "hidden" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."hidden" IS '@access: shared';
-- Set comment to column: "environment_id" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."environment_id" IS '@access: confidential';
-- Set comment to column: "version" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."version" IS '@access: shared';
-- Set comment to column: "updated_at" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."updated_at" IS '@access: shared';
-- Set comment to column: "id" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."id" IS '@access: confidential';

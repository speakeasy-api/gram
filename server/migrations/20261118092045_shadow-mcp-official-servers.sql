-- Create "shadow_mcp_official_servers" table
CREATE TABLE "shadow_mcp_official_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "server_url" text NOT NULL,
  "mcp_server_id" uuid NULL,
  "allow_direct_access" boolean NOT NULL DEFAULT false,
  "created_by_user_id" text NULL,
  "updated_by_user_id" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "shadow_mcp_official_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "shadow_mcp_official_servers_project_id_mcp_server_id_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "shadow_mcp_official_servers_server_url_check" CHECK (server_url <> ''::text)
);
-- Create index "shadow_mcp_official_servers_mcp_server_id_idx" to table: "shadow_mcp_official_servers"
CREATE INDEX "shadow_mcp_official_servers_mcp_server_id_idx" ON "shadow_mcp_official_servers" ("mcp_server_id") WHERE (mcp_server_id IS NOT NULL);
-- Create index "shadow_mcp_official_servers_project_id_server_url_key" to table: "shadow_mcp_official_servers"
CREATE UNIQUE INDEX "shadow_mcp_official_servers_project_id_server_url_key" ON "shadow_mcp_official_servers" ("project_id", "server_url") WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "server_url" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."server_url" IS '@access: restricted';
-- Set comment to column: "mcp_server_id" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "allow_direct_access" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."allow_direct_access" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "updated_by_user_id" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."updated_by_user_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "shadow_mcp_official_servers"
COMMENT ON COLUMN "shadow_mcp_official_servers"."deleted" IS '@access: confidential';

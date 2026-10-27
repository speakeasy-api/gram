-- Create "user_hidden_mcp_servers" table
CREATE TABLE "user_hidden_mcp_servers" (
  "organization_id" text NOT NULL,
  "user_id" text NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "user_id", "mcp_server_id"),
  CONSTRAINT "user_hidden_mcp_servers_mcp_server_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_hidden_mcp_servers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_hidden_mcp_servers_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "user_hidden_mcp_servers_project_id_mcp_server_id_idx" to table: "user_hidden_mcp_servers"
CREATE INDEX "user_hidden_mcp_servers_project_id_mcp_server_id_idx" ON "user_hidden_mcp_servers" ("project_id", "mcp_server_id");
-- Set comment to column: "organization_id" on table: "user_hidden_mcp_servers"
COMMENT ON COLUMN "user_hidden_mcp_servers"."organization_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "user_hidden_mcp_servers"
COMMENT ON COLUMN "user_hidden_mcp_servers"."user_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "user_hidden_mcp_servers"
COMMENT ON COLUMN "user_hidden_mcp_servers"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "user_hidden_mcp_servers"
COMMENT ON COLUMN "user_hidden_mcp_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "user_hidden_mcp_servers"
COMMENT ON COLUMN "user_hidden_mcp_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_hidden_mcp_servers"
COMMENT ON COLUMN "user_hidden_mcp_servers"."updated_at" IS '@access: confidential';

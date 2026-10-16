-- Create "mcp_server_remote_session_clients" table
CREATE TABLE "mcp_server_remote_session_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "remote_session_client_id" uuid NOT NULL,
  "client_attachment_scope" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_server_remote_session_clients_client_scope_fkey" FOREIGN KEY ("remote_session_client_id", "client_attachment_scope") REFERENCES "remote_session_clients" ("id", "attachment_scope") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_remote_session_clients_mcp_server_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_remote_session_clients_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_remote_session_clients_client_scope_check" CHECK (((client_attachment_scope = ('project:'::text || (project_id)::text)) OR (client_attachment_scope = ('organization:'::text || organization_id))) OR (client_attachment_scope = 'global'::text))
);
-- Create index "mcp_server_remote_session_clients_client_idx" to table: "mcp_server_remote_session_clients"
CREATE INDEX "mcp_server_remote_session_clients_client_idx" ON "mcp_server_remote_session_clients" ("remote_session_client_id");
-- Create index "mcp_server_remote_session_clients_mcp_server_id_idx" to table: "mcp_server_remote_session_clients"
CREATE INDEX "mcp_server_remote_session_clients_mcp_server_id_idx" ON "mcp_server_remote_session_clients" ("mcp_server_id");
-- Create index "mcp_server_remote_session_clients_project_id_idx" to table: "mcp_server_remote_session_clients"
CREATE INDEX "mcp_server_remote_session_clients_project_id_idx" ON "mcp_server_remote_session_clients" ("project_id");
-- Create index "mcp_server_remote_session_clients_server_client_key" to table: "mcp_server_remote_session_clients"
CREATE UNIQUE INDEX "mcp_server_remote_session_clients_server_client_key" ON "mcp_server_remote_session_clients" ("mcp_server_id", "remote_session_client_id") WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."organization_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "remote_session_client_id" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "client_attachment_scope" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."client_attachment_scope" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_server_remote_session_clients"
COMMENT ON COLUMN "mcp_server_remote_session_clients"."deleted" IS '@access: confidential';

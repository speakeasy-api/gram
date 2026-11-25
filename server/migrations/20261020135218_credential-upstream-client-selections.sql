-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "credential_id" uuid NULL;
-- Set comment to column: "credential_id" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."credential_id" IS '@access: confidential';
-- Create "upstream_client_selections" table
CREATE TABLE "upstream_client_selections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "credential_kind" text NOT NULL,
  "credential_id" uuid NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "remote_session_client_id" uuid NULL,
  "client_attachment_scope" text NULL,
  "no_auth" boolean NOT NULL DEFAULT false,
  "source" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "upstream_client_selections_client_scope_fkey" FOREIGN KEY ("remote_session_client_id", "client_attachment_scope") REFERENCES "remote_session_clients" ("id", "attachment_scope") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "upstream_client_selections_mcp_server_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "upstream_client_selections_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "upstream_client_selections_client_scope_check" CHECK (((remote_session_client_id IS NULL) AND (client_attachment_scope IS NULL)) OR ((remote_session_client_id IS NOT NULL) AND (client_attachment_scope IS NOT NULL) AND (((client_attachment_scope = ('project:'::text || (project_id)::text)) OR (client_attachment_scope = ('organization:'::text || organization_id))) OR (client_attachment_scope = 'global'::text)))),
  CONSTRAINT "upstream_client_selections_credential_kind_check" CHECK (credential_kind <> ''::text),
  CONSTRAINT "upstream_client_selections_no_auth_check" CHECK (no_auth = (remote_session_client_id IS NULL)),
  CONSTRAINT "upstream_client_selections_source_check" CHECK (source <> ''::text)
);
-- Create index "upstream_client_selections_client_idx" to table: "upstream_client_selections"
CREATE INDEX "upstream_client_selections_client_idx" ON "upstream_client_selections" ("remote_session_client_id");
-- Create index "upstream_client_selections_credential_server_key" to table: "upstream_client_selections"
CREATE UNIQUE INDEX "upstream_client_selections_credential_server_key" ON "upstream_client_selections" ("credential_kind", "credential_id", "mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "upstream_client_selections_mcp_server_idx" to table: "upstream_client_selections"
CREATE INDEX "upstream_client_selections_mcp_server_idx" ON "upstream_client_selections" ("mcp_server_id");
-- Create index "upstream_client_selections_project_id_idx" to table: "upstream_client_selections"
CREATE INDEX "upstream_client_selections_project_id_idx" ON "upstream_client_selections" ("project_id");
-- Set comment to column: "id" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."organization_id" IS '@access: confidential';
-- Set comment to column: "credential_kind" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."credential_kind" IS '@access: confidential';
-- Set comment to column: "credential_id" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."credential_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "remote_session_client_id" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "client_attachment_scope" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."client_attachment_scope" IS '@access: confidential';
-- Set comment to column: "no_auth" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."no_auth" IS '@access: confidential';
-- Set comment to column: "source" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."source" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."deleted" IS '@access: confidential';

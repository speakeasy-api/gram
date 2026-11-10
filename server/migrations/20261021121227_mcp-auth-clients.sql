-- Create "mcp_auth_clients" table
CREATE TABLE "mcp_auth_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "attachment_scope" text NULL GENERATED ALWAYS AS (
CASE
    WHEN (project_id IS NOT NULL) THEN ('project:'::text || (project_id)::text)
    ELSE ('organization:'::text || organization_id)
END) STORED,
  "client_type" text NOT NULL,
  "source" text NOT NULL,
  "name" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "created_by_user_id" text NULL,
  "remote_session_client_id" uuid NULL,
  "client_attachment_scope" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_auth_clients_client_scope_fkey" FOREIGN KEY ("remote_session_client_id", "client_attachment_scope") REFERENCES "remote_session_clients" ("id", "attachment_scope") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_auth_clients_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_auth_clients_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_auth_clients_client_type_check" CHECK (client_type <> ''::text),
  CONSTRAINT "mcp_auth_clients_name_check" CHECK (name <> ''::text),
  CONSTRAINT "mcp_auth_clients_registration_check" CHECK (((remote_session_client_id IS NULL) = (client_attachment_scope IS NULL)) AND ((client_attachment_scope IS NULL) OR (((client_attachment_scope = attachment_scope) OR (client_attachment_scope = ('organization:'::text || organization_id))) OR (client_attachment_scope = 'global'::text)))),
  CONSTRAINT "mcp_auth_clients_source_check" CHECK (source <> ''::text)
);
-- Create index "mcp_auth_clients_attachment_scope_key" to table: "mcp_auth_clients"
CREATE UNIQUE INDEX "mcp_auth_clients_attachment_scope_key" ON "mcp_auth_clients" ("id", "attachment_scope");
-- Create index "mcp_auth_clients_organization_id_idx" to table: "mcp_auth_clients"
CREATE INDEX "mcp_auth_clients_organization_id_idx" ON "mcp_auth_clients" ("organization_id") WHERE (deleted IS FALSE);
-- Create index "mcp_auth_clients_organization_key" to table: "mcp_auth_clients"
CREATE UNIQUE INDEX "mcp_auth_clients_organization_key" ON "mcp_auth_clients" ("id", "organization_id");
-- Create index "mcp_auth_clients_project_id_idx" to table: "mcp_auth_clients"
CREATE INDEX "mcp_auth_clients_project_id_idx" ON "mcp_auth_clients" ("project_id") WHERE (deleted IS FALSE);
-- Create index "mcp_auth_clients_registration_idx" to table: "mcp_auth_clients"
CREATE INDEX "mcp_auth_clients_registration_idx" ON "mcp_auth_clients" ("organization_id", "remote_session_client_id") WHERE (deleted IS FALSE);
-- Create index "mcp_auth_clients_remote_session_registration_key" to table: "mcp_auth_clients"
CREATE UNIQUE INDEX "mcp_auth_clients_remote_session_registration_key" ON "mcp_auth_clients" ("organization_id", "remote_session_client_id") WHERE ((client_type = 'remote_session'::text) AND (source = 'backfill'::text) AND (deleted IS FALSE));
-- Create index "mcp_auth_clients_service_account_registration_key" to table: "mcp_auth_clients"
CREATE UNIQUE INDEX "mcp_auth_clients_service_account_registration_key" ON "mcp_auth_clients" ("organization_id", "remote_session_client_id") WHERE ((client_type = 'service_account'::text) AND (source = 'backfill'::text) AND (deleted IS FALSE));
-- Create index "mcp_auth_clients_type_registration_key" to table: "mcp_auth_clients"
CREATE UNIQUE INDEX "mcp_auth_clients_type_registration_key" ON "mcp_auth_clients" ("id", "client_type", "remote_session_client_id", "organization_id");
-- Set comment to column: "id" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."project_id" IS '@access: confidential';
-- Set comment to column: "attachment_scope" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."attachment_scope" IS '@access: confidential';
-- Set comment to column: "client_type" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."client_type" IS '@access: confidential';
-- Set comment to column: "source" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."source" IS '@access: confidential';
-- Set comment to column: "name" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."name" IS '@access: opaque-confidential';
-- Set comment to column: "enabled" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."enabled" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "remote_session_client_id" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "client_attachment_scope" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."client_attachment_scope" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_auth_clients"
COMMENT ON COLUMN "mcp_auth_clients"."deleted" IS '@access: confidential';
-- Create "mcp_auth_client_emas" table
CREATE TABLE "mcp_auth_client_emas" (
  "auth_client_id" uuid NOT NULL,
  "client_type" text NOT NULL DEFAULT 'ema',
  "organization_id" text NOT NULL,
  "remote_session_client_id" uuid NOT NULL,
  "remote_session_issuer_id" uuid NOT NULL,
  "resource" text NOT NULL,
  "requested_scopes" text[] NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("auth_client_id"),
  CONSTRAINT "mcp_auth_client_emas_auth_client_fkey" FOREIGN KEY ("auth_client_id", "client_type", "remote_session_client_id", "organization_id") REFERENCES "mcp_auth_clients" ("id", "client_type", "remote_session_client_id", "organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_auth_client_emas_client_issuer_fkey" FOREIGN KEY ("remote_session_client_id", "remote_session_issuer_id") REFERENCES "remote_session_clients" ("id", "remote_session_issuer_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_auth_client_emas_client_type_check" CHECK (client_type = 'ema'::text),
  CONSTRAINT "mcp_auth_client_emas_resource_check" CHECK (resource <> ''::text)
);
-- Create index "mcp_auth_client_emas_upstream_key" to table: "mcp_auth_client_emas"
CREATE UNIQUE INDEX "mcp_auth_client_emas_upstream_key" ON "mcp_auth_client_emas" ("organization_id", "remote_session_issuer_id", "remote_session_client_id", "resource") WHERE (deleted IS FALSE);
-- Set comment to column: "auth_client_id" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."auth_client_id" IS '@access: confidential';
-- Set comment to column: "client_type" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."client_type" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."organization_id" IS '@access: confidential';
-- Set comment to column: "remote_session_client_id" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "remote_session_issuer_id" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."remote_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "resource" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."resource" IS '@access: restricted';
-- Set comment to column: "requested_scopes" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."requested_scopes" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_auth_client_emas"
COMMENT ON COLUMN "mcp_auth_client_emas"."deleted" IS '@access: confidential';
-- Create "mcp_auth_client_headers" table
CREATE TABLE "mcp_auth_client_headers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "auth_client_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "name" text NOT NULL,
  "description" text NULL,
  "is_required" boolean NOT NULL DEFAULT false,
  "is_secret" boolean NOT NULL DEFAULT false,
  "value" text NULL,
  "value_from_request_header" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_auth_client_headers_auth_client_fkey" FOREIGN KEY ("auth_client_id", "organization_id") REFERENCES "mcp_auth_clients" ("id", "organization_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_auth_client_headers_name_check" CHECK (name <> ''::text),
  CONSTRAINT "mcp_auth_client_headers_value_from_request_header_check" CHECK ((value_from_request_header IS NULL) OR (value_from_request_header <> ''::text))
);
-- Create index "mcp_auth_client_headers_auth_client_id_name_key" to table: "mcp_auth_client_headers"
CREATE UNIQUE INDEX "mcp_auth_client_headers_auth_client_id_name_key" ON "mcp_auth_client_headers" ("auth_client_id", "name") WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."id" IS '@access: confidential';
-- Set comment to column: "auth_client_id" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."auth_client_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."organization_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."description" IS '@access: opaque-restricted';
-- Set comment to column: "is_required" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."is_required" IS '@access: confidential';
-- Set comment to column: "is_secret" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."is_secret" IS '@access: confidential';
-- Set comment to column: "value" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."value" IS '@access: secret-restricted';
-- Set comment to column: "value_from_request_header" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."value_from_request_header" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."deleted" IS '@access: confidential';
-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" ADD COLUMN "requires_upstream_auth" boolean NULL;
-- Set comment to column: "requires_upstream_auth" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."requires_upstream_auth" IS 'Whether the upstream needs authentication: true needs it, false explicitly needs none, NULL is undecided.
@access: confidential';
-- Create "mcp_server_auth_clients" table
CREATE TABLE "mcp_server_auth_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "auth_client_id" uuid NOT NULL,
  "client_attachment_scope" text NOT NULL,
  "source" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_server_auth_clients_auth_client_scope_fkey" FOREIGN KEY ("auth_client_id", "client_attachment_scope") REFERENCES "mcp_auth_clients" ("id", "attachment_scope") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_auth_clients_mcp_server_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_auth_clients_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_auth_clients_client_scope_check" CHECK ((client_attachment_scope = ('project:'::text || (project_id)::text)) OR (client_attachment_scope = ('organization:'::text || organization_id))),
  CONSTRAINT "mcp_server_auth_clients_source_check" CHECK (source <> ''::text)
);
-- Create index "mcp_server_auth_clients_auth_client_idx" to table: "mcp_server_auth_clients"
CREATE INDEX "mcp_server_auth_clients_auth_client_idx" ON "mcp_server_auth_clients" ("auth_client_id");
-- Create index "mcp_server_auth_clients_project_id_idx" to table: "mcp_server_auth_clients"
CREATE INDEX "mcp_server_auth_clients_project_id_idx" ON "mcp_server_auth_clients" ("project_id");
-- Create index "mcp_server_auth_clients_server_client_key" to table: "mcp_server_auth_clients"
CREATE UNIQUE INDEX "mcp_server_auth_clients_server_client_key" ON "mcp_server_auth_clients" ("mcp_server_id", "auth_client_id") WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."organization_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "auth_client_id" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."auth_client_id" IS '@access: confidential';
-- Set comment to column: "client_attachment_scope" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."client_attachment_scope" IS '@access: confidential';
-- Set comment to column: "source" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."source" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_server_auth_clients"
COMMENT ON COLUMN "mcp_server_auth_clients"."deleted" IS '@access: confidential';

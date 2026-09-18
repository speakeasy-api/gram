-- Create "xaa_resource_readiness" table
CREATE TABLE "xaa_resource_readiness" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "identity_provider_connection_id" uuid NOT NULL,
  "remote_session_issuer_id" uuid NOT NULL,
  "resource_source" text NOT NULL DEFAULT 'custom',
  "scope_policy" text NULL,
  "connection_confirmed_at" timestamptz NULL,
  "connection_confirmed_by" text NULL,
  "resource_xaa_confirmed_at" timestamptz NULL,
  "last_exchange_outcome" text NULL,
  "last_exchange_at" timestamptz NULL,
  "verified_at" timestamptz NULL,
  "last_error_reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "xaa_resource_readiness_server_connection_key" UNIQUE ("organization_id", "project_id", "mcp_server_id", "identity_provider_connection_id"),
  CONSTRAINT "xaa_resource_readiness_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "xaa_resource_readiness_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "xaa_resource_readiness_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "xaa_resource_readiness_project_server_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "xaa_resource_readiness_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "xaa_resource_readiness_connection_confirmed_check" CHECK (((connection_confirmed_at IS NULL) = (connection_confirmed_by IS NULL)) AND ((connection_confirmed_by IS NULL) OR (connection_confirmed_by <> ''::text))),
  CONSTRAINT "xaa_resource_readiness_resource_source_check" CHECK (resource_source = ANY (ARRAY['custom'::text, 'oin'::text]))
);
-- Create index "xaa_resource_readiness_connection_idx" to table: "xaa_resource_readiness"
CREATE INDEX "xaa_resource_readiness_connection_idx" ON "xaa_resource_readiness" ("organization_id", "identity_provider_connection_id");
-- Create index "xaa_resource_readiness_project_server_idx" to table: "xaa_resource_readiness"
CREATE INDEX "xaa_resource_readiness_project_server_idx" ON "xaa_resource_readiness" ("project_id", "mcp_server_id");
-- Create index "xaa_resource_readiness_remote_session_issuer_idx" to table: "xaa_resource_readiness"
CREATE INDEX "xaa_resource_readiness_remote_session_issuer_idx" ON "xaa_resource_readiness" ("remote_session_issuer_id");

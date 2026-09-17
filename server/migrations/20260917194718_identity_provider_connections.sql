-- atlas:txmode none

-- Create "identity_provider_connections" table
CREATE TABLE "identity_provider_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "provider" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "last_verified_at" timestamptz NULL,
  "last_error" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "identity_provider_connections_id_provider_key" UNIQUE ("id", "provider"),
  CONSTRAINT "identity_provider_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "identity_provider_connections_status_check" CHECK (status = ANY (ARRAY['pending'::text, 'verified'::text, 'degraded'::text, 'revoked'::text]))
);
-- Create index "identity_provider_connections_organization_id_id_key" to table: "identity_provider_connections"
CREATE UNIQUE INDEX "identity_provider_connections_organization_id_id_key" ON "identity_provider_connections" ("organization_id", "id");
-- Create index "identity_provider_connections_organization_id_provider_key" to table: "identity_provider_connections"
CREATE UNIQUE INDEX "identity_provider_connections_organization_id_provider_key" ON "identity_provider_connections" ("organization_id", "provider") WHERE (deleted IS FALSE);
-- Modify "external_keys" table
ALTER TABLE "external_keys" ADD CONSTRAINT "external_keys_identity_provider_connection_id_check" CHECK ((identity_provider_connection_id IS NULL) OR (organization_id IS NOT NULL)) NOT VALID, ADD COLUMN "identity_provider_connection_id" uuid NULL, ADD CONSTRAINT "external_keys_identity_provider_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID;
-- Validate separately so the scan does not hold the ADD COLUMN lock.
ALTER TABLE "external_keys" VALIDATE CONSTRAINT "external_keys_identity_provider_connection_id_check";
ALTER TABLE "external_keys" VALIDATE CONSTRAINT "external_keys_identity_provider_connection_tenant_fkey";
-- Create index "external_keys_identity_provider_connection_idx" to table: "external_keys"
CREATE INDEX CONCURRENTLY "external_keys_identity_provider_connection_idx" ON "external_keys" ("organization_id", "identity_provider_connection_id") WHERE (identity_provider_connection_id IS NOT NULL);
-- Modify "json_web_key_sets" table
ALTER TABLE "json_web_key_sets" ADD COLUMN "identity_provider_connection_id" uuid NULL, ADD CONSTRAINT "json_web_key_sets_identity_provider_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID;
-- Validate separately so the scan does not hold the ADD COLUMN lock.
ALTER TABLE "json_web_key_sets" VALIDATE CONSTRAINT "json_web_key_sets_identity_provider_connection_tenant_fkey";
-- Create index "json_web_key_sets_identity_provider_connection_idx" to table: "json_web_key_sets"
CREATE INDEX CONCURRENTLY "json_web_key_sets_identity_provider_connection_idx" ON "json_web_key_sets" ("organization_id", "identity_provider_connection_id") WHERE (identity_provider_connection_id IS NOT NULL);
-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD CONSTRAINT "remote_session_clients_identity_provider_connection_id_check" CHECK ((identity_provider_connection_id IS NULL) OR (organization_id IS NOT NULL)) NOT VALID, ADD COLUMN "identity_provider_connection_id" uuid NULL, ADD CONSTRAINT "remote_session_clients_identity_provider_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID;
-- Validate separately so the scan does not hold the ADD COLUMN lock.
ALTER TABLE "remote_session_clients" VALIDATE CONSTRAINT "remote_session_clients_identity_provider_connection_id_check";
ALTER TABLE "remote_session_clients" VALIDATE CONSTRAINT "remote_session_clients_identity_provider_connection_tenant_fkey";
-- Create index "remote_session_clients_identity_provider_connection_idx" to table: "remote_session_clients"
CREATE INDEX CONCURRENTLY "remote_session_clients_identity_provider_connection_idx" ON "remote_session_clients" ("organization_id", "identity_provider_connection_id") WHERE (identity_provider_connection_id IS NOT NULL);
-- Create index "remote_session_clients_issuer_attachment_scope_key" to table: "remote_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_clients_issuer_attachment_scope_key" ON "remote_session_clients" ("id", "remote_session_issuer_id", "attachment_scope");
-- Create index "remote_session_clients_organization_id_id_key" to table: "remote_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_clients_organization_id_id_key" ON "remote_session_clients" ("organization_id", "id");
-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "attachment_scope" text NULL GENERATED ALWAYS AS (
CASE
    WHEN (project_id IS NOT NULL) THEN ('project:'::text || (project_id)::text)
    WHEN (organization_id IS NOT NULL) THEN ('organization:'::text || organization_id)
    ELSE 'global'::text
END) STORED;
-- Create index "remote_session_issuers_attachment_scope_key" to table: "remote_session_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_issuers_attachment_scope_key" ON "remote_session_issuers" ("id", "issuer", "attachment_scope");
-- Create index "remote_session_issuers_organization_id_id_key" to table: "remote_session_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_issuers_organization_id_id_key" ON "remote_session_issuers" ("organization_id", "id");
-- Create "okta_identity_provider_connections" table
CREATE TABLE "okta_identity_provider_connections" (
  "identity_provider_connection_id" uuid NOT NULL,
  "identity_provider_connections_provider" text NOT NULL DEFAULT 'okta',
  "organization_id" text NOT NULL,
  "attachment_scope" text NULL GENERATED ALWAYS AS ('organization:'::text || organization_id) STORED,
  "org_url" text NOT NULL,
  "issuer_url" text NOT NULL,
  "issuer_url_override_reason" text NULL,
  "ownership_claimed" boolean NOT NULL DEFAULT true,
  "remote_session_issuer_id" uuid NOT NULL,
  "remote_session_client_id" uuid NOT NULL,
  "dpop_required" boolean NOT NULL DEFAULT false,
  "granted_scopes" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "observed_admin_roles" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "listing_mode" text NOT NULL DEFAULT 'custom_app',
  "agent_id" text NULL,
  "agent_app_id" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("identity_provider_connection_id"),
  CONSTRAINT "okta_identity_provider_connections_client_issuer_scope_fkey" FOREIGN KEY ("remote_session_client_id", "remote_session_issuer_id", "attachment_scope") REFERENCES "remote_session_clients" ("id", "remote_session_issuer_id", "attachment_scope") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "okta_identity_provider_connections_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_identity_provider_connections_fkey" FOREIGN KEY ("identity_provider_connection_id", "identity_provider_connections_provider") REFERENCES "identity_provider_connections" ("id", "provider") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_identity_provider_connections_issuer_scope_fkey" FOREIGN KEY ("remote_session_issuer_id", "issuer_url", "attachment_scope") REFERENCES "remote_session_issuers" ("id", "issuer", "attachment_scope") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "okta_identity_provider_connections_listing_mode_check" CHECK (listing_mode = ANY (ARRAY['custom_app'::text, 'oin'::text])),
  CONSTRAINT "okta_identity_provider_connections_override_reason_check" CHECK ((issuer_url_override_reason IS NULL) OR (issuer_url_override_reason ~ '[^[:space:]]'::text)),
  CONSTRAINT "okta_identity_provider_connections_provider_check" CHECK (identity_provider_connections_provider = 'okta'::text)
);
-- Create index "okta_identity_provider_connections_issuer_url_key" to table: "okta_identity_provider_connections"
CREATE UNIQUE INDEX "okta_identity_provider_connections_issuer_url_key" ON "okta_identity_provider_connections" ("issuer_url") WHERE ((deleted IS FALSE) AND (ownership_claimed IS TRUE) AND (issuer_url_override_reason IS NULL));
-- Create index "okta_identity_provider_connections_remote_session_client_idx" to table: "okta_identity_provider_connections"
CREATE INDEX "okta_identity_provider_connections_remote_session_client_idx" ON "okta_identity_provider_connections" ("organization_id", "remote_session_client_id");
-- Create index "okta_identity_provider_connections_remote_session_issuer_idx" to table: "okta_identity_provider_connections"
CREATE INDEX "okta_identity_provider_connections_remote_session_issuer_idx" ON "okta_identity_provider_connections" ("organization_id", "remote_session_issuer_id");

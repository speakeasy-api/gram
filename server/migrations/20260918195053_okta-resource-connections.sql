-- Create "okta_resource_connections" table
CREATE TABLE "okta_resource_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "identity_provider_connection_id" uuid NOT NULL,
  "remote_session_issuer_id" uuid NOT NULL,
  "resource" text NOT NULL,
  "audience" text NOT NULL,
  "okta_application_id" text NULL,
  "last_exchange_outcome" text NULL,
  "last_exchange_at" timestamptz NULL,
  "verified_at" timestamptz NULL,
  "last_error_reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "okta_resource_connections_resource_key" UNIQUE ("organization_id", "identity_provider_connection_id", "remote_session_issuer_id", "resource"),
  CONSTRAINT "okta_resource_connections_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_resource_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_resource_connections_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_resource_connections_audience_check" CHECK (audience <> ''::text),
  CONSTRAINT "okta_resource_connections_last_exchange_outcome_check" CHECK ((last_exchange_outcome IS NULL) OR (last_exchange_outcome = ANY (ARRAY['succeeded'::text, 'broken'::text, 'failed'::text]))),
  CONSTRAINT "okta_resource_connections_okta_application_id_check" CHECK ((okta_application_id IS NULL) OR (okta_application_id <> ''::text)),
  CONSTRAINT "okta_resource_connections_resource_check" CHECK (resource <> ''::text)
);
-- Create index "okta_resource_connections_remote_session_issuer_idx" to table: "okta_resource_connections"
CREATE INDEX "okta_resource_connections_remote_session_issuer_idx" ON "okta_resource_connections" ("remote_session_issuer_id");

-- Create "xaa_resource_readiness" table
CREATE TABLE "xaa_resource_readiness" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "identity_provider_connection_id" uuid NOT NULL,
  "remote_session_issuer_id" uuid NOT NULL,
  "resource" text NOT NULL,
  "resource_source" text NOT NULL DEFAULT 'custom',
  "last_exchange_outcome" text NULL,
  "last_exchange_at" timestamptz NULL,
  "verified_at" timestamptz NULL,
  "last_error_reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "xaa_resource_readiness_resource_key" UNIQUE ("organization_id", "identity_provider_connection_id", "remote_session_issuer_id", "resource"),
  CONSTRAINT "xaa_resource_readiness_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "xaa_resource_readiness_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "xaa_resource_readiness_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "xaa_resource_readiness_last_exchange_outcome_check" CHECK ((last_exchange_outcome IS NULL) OR (last_exchange_outcome = ANY (ARRAY['succeeded'::text, 'broken'::text, 'failed'::text]))),
  CONSTRAINT "xaa_resource_readiness_resource_check" CHECK (resource <> ''::text),
  CONSTRAINT "xaa_resource_readiness_resource_source_check" CHECK (resource_source = ANY (ARRAY['custom'::text, 'oin'::text]))
);
-- Create index "xaa_resource_readiness_remote_session_issuer_idx" to table: "xaa_resource_readiness"
CREATE INDEX "xaa_resource_readiness_remote_session_issuer_idx" ON "xaa_resource_readiness" ("remote_session_issuer_id");

-- Create "identity_provider_connections" table
CREATE TABLE "identity_provider_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "kind" text NOT NULL,
  "tenant_identifier" text NOT NULL,
  "display_name" text NULL,
  "status" text NOT NULL,
  "status_detail" text NULL,
  "capabilities" text[] NOT NULL DEFAULT '{}',
  "last_verified_at" timestamptz NULL,
  "verify_evidence" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "identity_provider_connections_id_kind_key" UNIQUE ("id", "kind"),
  CONSTRAINT "identity_provider_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "identity_provider_connections_organization_id_key" to table: "identity_provider_connections"
CREATE UNIQUE INDEX "identity_provider_connections_organization_id_key" ON "identity_provider_connections" ("organization_id") WHERE (deleted IS FALSE);
-- Create "identity_provider_signing_keys" table
CREATE TABLE "identity_provider_signing_keys" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "identity_provider_connection_id" uuid NOT NULL,
  "kid" text NOT NULL,
  "algorithm" text NOT NULL,
  "public_jwk" jsonb NOT NULL,
  "private_key_encrypted" text NOT NULL,
  "state" text NOT NULL,
  "activated_at" timestamptz NULL,
  "retired_at" timestamptz NULL,
  "last_used_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "identity_provider_signing_keys_connection_id_kid_key" UNIQUE ("identity_provider_connection_id", "kid"),
  CONSTRAINT "identity_provider_signing_keys_connection_id_fkey" FOREIGN KEY ("identity_provider_connection_id") REFERENCES "identity_provider_connections" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "identity_provider_signing_keys_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "okta_identity_provider_connections" table
CREATE TABLE "okta_identity_provider_connections" (
  "identity_provider_connection_id" uuid NOT NULL,
  "identity_provider_connections_kind" text NOT NULL DEFAULT 'okta',
  "okta_domain" text NOT NULL,
  "auth_method" text NOT NULL,
  "client_id" text NULL,
  "signing_key_id" uuid NULL,
  "granted_scopes" text[] NOT NULL DEFAULT '{}',
  "sign_in_application_id" text NULL,
  "workos_connection_id" text NULL,
  "sign_in_state" text NULL,
  "groups_source" text NULL,
  "groups_claim_confirmed" boolean NOT NULL DEFAULT false,
  "sign_in_evidence" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT "okta_idp_connections_pkey" PRIMARY KEY ("identity_provider_connection_id"),
  CONSTRAINT "okta_idp_connections_connection_kind_fkey" FOREIGN KEY ("identity_provider_connection_id", "identity_provider_connections_kind") REFERENCES "identity_provider_connections" ("id", "kind") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_idp_connections_signing_key_id_fkey" FOREIGN KEY ("signing_key_id") REFERENCES "identity_provider_signing_keys" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "okta_idp_connections_kind_check" CHECK (identity_provider_connections_kind = 'okta'::text)
);

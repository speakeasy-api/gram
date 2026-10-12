-- Create "internal_session_issuers" table
CREATE TABLE "internal_session_issuers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "json_web_key_set_id" uuid NOT NULL,
  "disabled_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "internal_session_issuers_key_set_tenant_fkey" FOREIGN KEY ("organization_id", "json_web_key_set_id") REFERENCES "json_web_key_sets" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "internal_session_issuers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "internal_session_issuers_key_set_idx" to table: "internal_session_issuers"
CREATE INDEX "internal_session_issuers_key_set_idx" ON "internal_session_issuers" ("organization_id", "json_web_key_set_id");
-- Create index "internal_session_issuers_organization_id_id_key" to table: "internal_session_issuers"
CREATE UNIQUE INDEX "internal_session_issuers_organization_id_id_key" ON "internal_session_issuers" ("organization_id", "id");
-- Create index "internal_session_issuers_organization_id_key" to table: "internal_session_issuers"
CREATE UNIQUE INDEX "internal_session_issuers_organization_id_key" ON "internal_session_issuers" ("organization_id") WHERE (deleted IS FALSE);
-- Create "ema_vendor_identity_providers" table
CREATE TABLE "ema_vendor_identity_providers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "vendor_issuer" text NOT NULL,
  "identity_provider_connection_id" uuid NULL,
  "internal_session_issuer_id" uuid NULL,
  "position" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "ema_vendor_identity_providers_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "ema_vendor_identity_providers_issuer_tenant_fkey" FOREIGN KEY ("organization_id", "internal_session_issuer_id") REFERENCES "internal_session_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "ema_vendor_identity_providers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "ema_vendor_identity_providers_one_provider_check" CHECK (num_nonnulls(identity_provider_connection_id, internal_session_issuer_id) = 1)
);
-- Create index "ema_vendor_identity_providers_connection_idx" to table: "ema_vendor_identity_providers"
CREATE INDEX "ema_vendor_identity_providers_connection_idx" ON "ema_vendor_identity_providers" ("organization_id", "identity_provider_connection_id") WHERE (identity_provider_connection_id IS NOT NULL);
-- Create index "ema_vendor_identity_providers_issuer_idx" to table: "ema_vendor_identity_providers"
CREATE INDEX "ema_vendor_identity_providers_issuer_idx" ON "ema_vendor_identity_providers" ("organization_id", "internal_session_issuer_id") WHERE (internal_session_issuer_id IS NOT NULL);
-- Create index "ema_vendor_identity_providers_position_key" to table: "ema_vendor_identity_providers"
CREATE UNIQUE INDEX "ema_vendor_identity_providers_position_key" ON "ema_vendor_identity_providers" ("organization_id", "vendor_issuer", "position") WHERE (deleted IS FALSE);
-- Create index "ema_vendor_identity_providers_provider_key" to table: "ema_vendor_identity_providers"
CREATE UNIQUE INDEX "ema_vendor_identity_providers_provider_key" ON "ema_vendor_identity_providers" ("organization_id", "vendor_issuer", "identity_provider_connection_id", "internal_session_issuer_id") NULLS NOT DISTINCT WHERE (deleted IS FALSE);

-- atlas:txmode none

-- Modify "json_web_key_sets" table
ALTER TABLE "json_web_key_sets" ADD COLUMN "managed_ema_issuer" boolean NULL;
-- Create index "json_web_key_sets_managed_ema_issuer_key" to table: "json_web_key_sets"
CREATE UNIQUE INDEX CONCURRENTLY "json_web_key_sets_managed_ema_issuer_key" ON "json_web_key_sets" ("organization_id") WHERE ((managed_ema_issuer IS TRUE) AND (deleted IS FALSE));
-- Create "ema_vendor_identity_providers" table
CREATE TABLE "ema_vendor_identity_providers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "vendor_issuer" text NOT NULL,
  "provider_kind" text NOT NULL,
  "identity_provider_connection_id" uuid NULL,
  "position" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "ema_vendor_identity_providers_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "identity_provider_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "ema_vendor_identity_providers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "ema_vendor_identity_providers_connection_idx" to table: "ema_vendor_identity_providers"
CREATE INDEX "ema_vendor_identity_providers_connection_idx" ON "ema_vendor_identity_providers" ("organization_id", "identity_provider_connection_id") WHERE (identity_provider_connection_id IS NOT NULL);
-- Create index "ema_vendor_identity_providers_position_key" to table: "ema_vendor_identity_providers"
CREATE UNIQUE INDEX "ema_vendor_identity_providers_position_key" ON "ema_vendor_identity_providers" ("organization_id", "vendor_issuer", "position") WHERE (deleted IS FALSE);
-- Create index "ema_vendor_identity_providers_provider_key" to table: "ema_vendor_identity_providers"
CREATE UNIQUE INDEX "ema_vendor_identity_providers_provider_key" ON "ema_vendor_identity_providers" ("organization_id", "vendor_issuer", "provider_kind", "identity_provider_connection_id") NULLS NOT DISTINCT WHERE (deleted IS FALSE);

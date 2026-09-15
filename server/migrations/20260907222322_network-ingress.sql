-- atlas:txmode none

-- Create index "custom_domains_organization_id_id_key" to table: "custom_domains"
CREATE UNIQUE INDEX CONCURRENTLY "custom_domains_organization_id_id_key" ON "custom_domains" ("organization_id", "id");
-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" ADD COLUMN "network_access_mode" text NULL;
-- Modify "meta_mcp_servers" table
ALTER TABLE "meta_mcp_servers" ADD COLUMN "network_access_mode" text NULL;
-- Create "network_ingresses" table
CREATE TABLE "network_ingresses" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "provider" text NOT NULL,
  "hostname" text NOT NULL,
  "endpoint_namespace_kind" text NOT NULL,
  "custom_domain_id" uuid NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "identity_required" boolean NOT NULL DEFAULT false,
  "credentials_encrypted" text NULL,
  "attestor_namespace" text NOT NULL,
  "attestor_service_account" text NOT NULL,
  "provider_resources" jsonb NOT NULL DEFAULT '{}',
  "status" text NOT NULL DEFAULT 'pending',
  "dns_name" text NULL,
  "last_error" text NULL,
  "health_checked_at" timestamptz NULL,
  "connected_since" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "network_ingresses_organization_id_custom_domain_id_fkey" FOREIGN KEY ("organization_id", "custom_domain_id") REFERENCES "custom_domains" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "network_ingresses_attestor_namespace_service_account_key" to table: "network_ingresses"
CREATE UNIQUE INDEX "network_ingresses_attestor_namespace_service_account_key" ON "network_ingresses" ("attestor_namespace", "attestor_service_account") WHERE (deleted IS FALSE);
-- Create index "network_ingresses_custom_domain_id_idx" to table: "network_ingresses"
CREATE INDEX "network_ingresses_custom_domain_id_idx" ON "network_ingresses" ("custom_domain_id") WHERE ((deleted IS FALSE) AND (custom_domain_id IS NOT NULL));
-- Create index "network_ingresses_dns_name_idx" to table: "network_ingresses"
CREATE INDEX "network_ingresses_dns_name_idx" ON "network_ingresses" ("dns_name") WHERE ((deleted IS FALSE) AND (dns_name IS NOT NULL));
-- Create index "network_ingresses_organization_id_custom_domain_id_idx" to table: "network_ingresses"
CREATE INDEX "network_ingresses_organization_id_custom_domain_id_idx" ON "network_ingresses" ("organization_id", "custom_domain_id");
-- Create index "network_ingresses_organization_id_key" to table: "network_ingresses"
CREATE UNIQUE INDEX "network_ingresses_organization_id_key" ON "network_ingresses" ("organization_id") WHERE (deleted IS FALSE);

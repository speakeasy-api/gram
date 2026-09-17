-- atlas:txmode none

-- Drop index "okta_identity_provider_connections_issuer_url_key" from table: "okta_identity_provider_connections"
DROP INDEX CONCURRENTLY "okta_identity_provider_connections_issuer_url_key";
-- Modify "okta_identity_provider_connections" table
ALTER TABLE "okta_identity_provider_connections" ADD COLUMN "ownership_claimed" boolean NOT NULL DEFAULT true;
-- Create index "okta_identity_provider_connections_issuer_url_key" to table: "okta_identity_provider_connections"
CREATE UNIQUE INDEX CONCURRENTLY "okta_identity_provider_connections_issuer_url_key" ON "okta_identity_provider_connections" ("issuer_url") WHERE ((deleted IS FALSE) AND (ownership_claimed IS TRUE) AND (issuer_url_override_reason IS NULL));

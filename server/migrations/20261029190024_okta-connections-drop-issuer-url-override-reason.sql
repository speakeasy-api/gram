-- atlas:nolint DS103
-- Modify "okta_identity_provider_connections" table
ALTER TABLE "okta_identity_provider_connections" DROP CONSTRAINT "okta_identity_provider_connections_override_reason_check", DROP COLUMN "issuer_url_override_reason";

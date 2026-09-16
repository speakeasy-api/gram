-- atlas:txmode none

-- Create index "remote_session_clients_issuer_attachment_scope_key" to table: "remote_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_clients_issuer_attachment_scope_key" ON "remote_session_clients" ("id", "remote_session_issuer_id", "attachment_scope");
-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "attachment_scope" text NULL GENERATED ALWAYS AS (
CASE
    WHEN (project_id IS NOT NULL) THEN ('project:'::text || (project_id)::text)
    WHEN (organization_id IS NOT NULL) THEN ('organization:'::text || organization_id)
    ELSE 'global'::text
END) STORED;
-- Create index "remote_session_issuers_attachment_scope_key" to table: "remote_session_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_issuers_attachment_scope_key" ON "remote_session_issuers" ("id", "issuer", "attachment_scope");
-- Modify "okta_identity_provider_connections" table
ALTER TABLE "okta_identity_provider_connections" DROP CONSTRAINT "okta_identity_provider_connections_client_tenant_fkey", DROP CONSTRAINT "okta_identity_provider_connections_issuer_tenant_fkey", ADD CONSTRAINT "okta_identity_provider_connections_override_reason_check" CHECK ((issuer_url_override_reason IS NULL) OR (issuer_url_override_reason ~ '[^[:space:]]'::text)), ADD COLUMN "attachment_scope" text NULL GENERATED ALWAYS AS ('organization:'::text || organization_id) STORED, ADD CONSTRAINT "okta_identity_provider_connections_client_issuer_scope_fkey" FOREIGN KEY ("remote_session_client_id", "remote_session_issuer_id", "attachment_scope") REFERENCES "remote_session_clients" ("id", "remote_session_issuer_id", "attachment_scope") ON UPDATE NO ACTION ON DELETE NO ACTION, ADD CONSTRAINT "okta_identity_provider_connections_issuer_scope_fkey" FOREIGN KEY ("remote_session_issuer_id", "issuer_url", "attachment_scope") REFERENCES "remote_session_issuers" ("id", "issuer", "attachment_scope") ON UPDATE NO ACTION ON DELETE NO ACTION;

-- Modify "okta_identity_provider_connections" table
ALTER TABLE "okta_identity_provider_connections" ADD COLUMN "sign_in_application_id" text NULL, ADD COLUMN "workos_connection_id" text NULL, ADD COLUMN "sign_in_state" text NULL, ADD COLUMN "groups_source" text NULL, ADD COLUMN "groups_claim_confirmed" boolean NOT NULL DEFAULT false, ADD COLUMN "sign_in_evidence" jsonb NULL;

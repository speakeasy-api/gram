-- Modify "okta_identity_provider_connections" table
ALTER TABLE "okta_identity_provider_connections" ADD COLUMN "directory_application_id" text NULL, ADD COLUMN "directory_state" text NULL, ADD COLUMN "directory_group_count" integer NULL, ADD COLUMN "directory_user_count" integer NULL, ADD COLUMN "directory_evidence" jsonb NULL;
-- Modify "organization_onboarding" table
ALTER TABLE "organization_onboarding" ADD COLUMN "directory_scim_base_url" text NULL, ADD COLUMN "directory_scim_token_encrypted" text NULL, ADD COLUMN "directory_scim_token_key_id" uuid NULL, ADD COLUMN "directory_scim_token_fingerprint" text NULL, ADD COLUMN "directory_workos_id" text NULL, ADD COLUMN "directory_handoff_set_by_user_id" text NULL, ADD COLUMN "directory_handoff_updated_at" timestamptz NULL;

-- atlas:txmode none

-- Modify "trusted_issuer_sessions" table
ALTER TABLE "trusted_issuer_sessions" ADD COLUMN "credential_generation" bigint NULL DEFAULT 1, ADD COLUMN "refresh_claim_id" uuid NULL, ADD COLUMN "upstream_subject_encrypted" text NULL, ADD COLUMN "nonce_encrypted" text NULL, ADD COLUMN "credential_config_hash" text NULL, ADD COLUMN "observation_status" text NULL, ADD COLUMN "observed_at" timestamptz NULL, ADD COLUMN "credential_obtained_at" timestamptz NULL, ADD COLUMN "last_refresh_succeeded_at" timestamptz NULL, ADD COLUMN "retry_after" timestamptz NULL;
-- Create index "trusted_issuer_sessions_assertion_cleanup_idx" to table: "trusted_issuer_sessions"
CREATE INDEX CONCURRENTLY "trusted_issuer_sessions_assertion_cleanup_idx" ON "trusted_issuer_sessions" ("identity_assertion_expires_at" NULLS FIRST, "id") WHERE (identity_assertion_encrypted IS NOT NULL);
-- Create index "trusted_issuer_sessions_metadata_cleanup_idx" to table: "trusted_issuer_sessions"
CREATE INDEX CONCURRENTLY "trusted_issuer_sessions_metadata_cleanup_idx" ON "trusted_issuer_sessions" ("id") WHERE ((identity_assertion_encrypted IS NULL) AND (refresh_token_encrypted IS NULL) AND ((upstream_subject_encrypted IS NOT NULL) OR (nonce_encrypted IS NOT NULL)));
-- Create index "trusted_issuer_sessions_refresh_cleanup_idx" to table: "trusted_issuer_sessions"
CREATE INDEX CONCURRENTLY "trusted_issuer_sessions_refresh_cleanup_idx" ON "trusted_issuer_sessions" ("refresh_expires_at", "id") WHERE (refresh_token_encrypted IS NOT NULL);

-- atlas:txmode none

-- Create index "remote_sessions_organization_id_idx" to table: "remote_sessions"
CREATE INDEX CONCURRENTLY "remote_sessions_organization_id_idx" ON "remote_sessions" ("organization_id");
-- Create index "remote_sessions_recheck_due_v2_idx" to table: "remote_sessions"
CREATE INDEX CONCURRENTLY "remote_sessions_recheck_due_v2_idx" ON "remote_sessions" ((COALESCE(last_validated_at, created_at)), "id") WHERE ((deleted IS FALSE) AND (refresh_token_encrypted IS NULL) AND (refresh_token_org_encrypted IS NULL) AND (refresh_expires_at IS NULL));
-- Create index "remote_sessions_refresh_keepalive_due_v2_idx" to table: "remote_sessions"
CREATE INDEX CONCURRENTLY "remote_sessions_refresh_keepalive_due_v2_idx" ON "remote_sessions" ("updated_at", "id") WHERE ((deleted IS FALSE) AND ((refresh_token_encrypted IS NOT NULL) OR (refresh_token_org_encrypted IS NOT NULL)) AND (auto_refresh IS TRUE));
-- Create index "trusted_issuer_sessions_assertion_cleanup_v2_idx" to table: "trusted_issuer_sessions"
CREATE INDEX CONCURRENTLY "trusted_issuer_sessions_assertion_cleanup_v2_idx" ON "trusted_issuer_sessions" ("identity_assertion_expires_at" NULLS FIRST, "id") WHERE ((identity_assertion_encrypted IS NOT NULL) OR (identity_assertion_org_encrypted IS NOT NULL));
-- Create index "trusted_issuer_sessions_metadata_cleanup_v2_idx" to table: "trusted_issuer_sessions"
CREATE INDEX CONCURRENTLY "trusted_issuer_sessions_metadata_cleanup_v2_idx" ON "trusted_issuer_sessions" ("id") WHERE ((identity_assertion_encrypted IS NULL) AND (identity_assertion_org_encrypted IS NULL) AND (refresh_token_encrypted IS NULL) AND (refresh_token_org_encrypted IS NULL) AND ((upstream_subject_encrypted IS NOT NULL) OR (upstream_subject_org_encrypted IS NOT NULL) OR (nonce_encrypted IS NOT NULL) OR (nonce_org_encrypted IS NOT NULL)));
-- Create index "trusted_issuer_sessions_refresh_cleanup_v2_idx" to table: "trusted_issuer_sessions"
CREATE INDEX CONCURRENTLY "trusted_issuer_sessions_refresh_cleanup_v2_idx" ON "trusted_issuer_sessions" ("refresh_expires_at", "id") WHERE ((refresh_token_encrypted IS NOT NULL) OR (refresh_token_org_encrypted IS NOT NULL));

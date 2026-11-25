-- atlas:txmode none

-- Create index "remote_sessions_recheck_due_idx" to table: "remote_sessions"
CREATE INDEX CONCURRENTLY "remote_sessions_recheck_due_idx" ON "remote_sessions" ((COALESCE(last_validated_at, created_at)), "id") WHERE ((deleted IS FALSE) AND (refresh_token_encrypted IS NULL) AND (refresh_expires_at IS NULL));

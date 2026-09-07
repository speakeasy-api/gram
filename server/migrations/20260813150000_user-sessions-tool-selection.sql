-- atlas:txmode none

-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "tool_selection" jsonb NULL;
-- Create index "user_sessions_user_session_issuer_id_jti_idx" to table: "user_sessions"
CREATE INDEX CONCURRENTLY "user_sessions_user_session_issuer_id_jti_idx" ON "user_sessions" ("user_session_issuer_id", "jti") WHERE (deleted IS FALSE);

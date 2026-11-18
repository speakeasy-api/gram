-- atlas:txmode none

-- Create index "user_sessions_spent_refresh_token_hash_idx" to table: "user_sessions"
CREATE INDEX CONCURRENTLY "user_sessions_spent_refresh_token_hash_idx" ON "user_sessions" ("refresh_token_hash") WHERE ((deleted IS TRUE) AND (refresh_token_hash IS NOT NULL));

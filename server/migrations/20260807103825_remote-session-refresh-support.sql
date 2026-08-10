-- atlas:txmode none

-- Modify "remote_sessions" table
ALTER TABLE "remote_sessions" ADD COLUMN "authorization_expires_at" timestamptz NULL, ADD COLUMN "resource" text NULL, ADD COLUMN "auto_refresh" boolean NOT NULL DEFAULT false, ADD COLUMN "last_refresh_attempt_at" timestamptz NULL;
-- Create index "remote_sessions_refresh_keepalive_due_idx" to table: "remote_sessions"
CREATE INDEX CONCURRENTLY "remote_sessions_refresh_keepalive_due_idx" ON "remote_sessions" ("updated_at", "id") WHERE ((deleted IS FALSE) AND (refresh_token_encrypted IS NOT NULL) AND (auto_refresh IS TRUE));

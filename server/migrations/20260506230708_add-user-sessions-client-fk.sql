-- atlas:txmode none

-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "user_session_client_id" uuid NULL, ADD CONSTRAINT "user_sessions_user_session_client_id_fkey" FOREIGN KEY ("user_session_client_id") REFERENCES "user_session_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "user_sessions_user_session_client_id_idx" to table: "user_sessions"
CREATE INDEX CONCURRENTLY "user_sessions_user_session_client_id_idx" ON "user_sessions" ("user_session_client_id") WHERE (deleted IS FALSE);

-- atlas:txmode none

-- Create index "remote_sessions_client_id_key" to table: "remote_sessions"
CREATE UNIQUE INDEX CONCURRENTLY "remote_sessions_client_id_key" ON "remote_sessions" ("remote_session_client_id", "id");
-- Modify "principal_remote_session_bindings" table
ALTER TABLE "principal_remote_session_bindings" DROP CONSTRAINT "principal_remote_session_bindings_session_fkey", ADD CONSTRAINT "principal_remote_session_bindings_client_session_fkey" FOREIGN KEY ("remote_session_client_id", "remote_session_id") REFERENCES "remote_sessions" ("remote_session_client_id", "id") ON UPDATE CASCADE ON DELETE CASCADE;

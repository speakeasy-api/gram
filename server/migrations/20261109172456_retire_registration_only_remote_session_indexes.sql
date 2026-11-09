-- atlas:txmode none

-- Drop index "remote_sessions_subject_client_issuer_key" from table: "remote_sessions"
DROP INDEX CONCURRENTLY "remote_sessions_subject_client_issuer_key";
-- Drop index "remote_sessions_subject_client_key" from table: "remote_sessions"
DROP INDEX CONCURRENTLY "remote_sessions_subject_client_key";

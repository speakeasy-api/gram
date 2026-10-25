-- atlas:txmode none

-- Create index "principal_remote_session_bindings_agent_subject_idx" to table: "principal_remote_session_bindings"
CREATE INDEX CONCURRENTLY "principal_remote_session_bindings_agent_subject_idx" ON "principal_remote_session_bindings" ((('agent:'::text || (principal_id)::text)), "user_session_issuer_id") WHERE (revoked_at IS NULL);

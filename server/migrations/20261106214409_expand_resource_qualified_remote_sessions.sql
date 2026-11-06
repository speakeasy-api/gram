-- atlas:txmode none

-- Create index "remote_sessions_subject_client_issuer_resource_key" to table: "remote_sessions"
CREATE UNIQUE INDEX CONCURRENTLY "remote_sessions_subject_client_issuer_resource_key" ON "remote_sessions" ("subject_urn", "remote_session_client_id", "user_session_issuer_id", (rtrim(resource, '/'::text))) NULLS NOT DISTINCT WHERE (deleted IS FALSE);
-- Create index "remote_sessions_subject_client_resource_key" to table: "remote_sessions"
CREATE UNIQUE INDEX CONCURRENTLY "remote_sessions_subject_client_resource_key" ON "remote_sessions" ("subject_urn", "remote_session_client_id", (rtrim(resource, '/'::text))) NULLS NOT DISTINCT WHERE (deleted IS FALSE);

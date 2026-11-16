-- atlas:txmode none

-- Create index "user_session_consent_resources_user_session_consent_id_idx" to table: "user_session_consent_resources"
CREATE INDEX CONCURRENTLY "user_session_consent_resources_user_session_consent_id_idx" ON "user_session_consent_resources" ("user_session_consent_id");

-- atlas:txmode none

-- Create index "toolsets_user_session_issuer_id_idx" to table: "toolsets"
CREATE INDEX CONCURRENTLY "toolsets_user_session_issuer_id_idx" ON "toolsets" ("user_session_issuer_id") WHERE (user_session_issuer_id IS NOT NULL);

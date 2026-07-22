-- atlas:txmode none

-- Create index "remote_session_issuers_issuer_idx" to table: "remote_session_issuers"
CREATE INDEX CONCURRENTLY "remote_session_issuers_issuer_idx" ON "remote_session_issuers" ("issuer") WHERE (deleted IS FALSE);

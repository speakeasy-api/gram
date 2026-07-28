-- atlas:txmode none

-- Create index "remote_session_issuers_global_slug_key" to table: "remote_session_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_issuers_global_slug_key" ON "remote_session_issuers" ("slug") WHERE ((deleted IS FALSE) AND (project_id IS NULL) AND (organization_id IS NULL));

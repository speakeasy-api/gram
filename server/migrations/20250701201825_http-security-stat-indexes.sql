-- atlas:txmode none

-- Create index "http_security_deleted_idx" to table: "http_security"
CREATE INDEX CONCURRENTLY "http_security_deleted_idx" ON "http_security" ("deleted");
-- Create index "http_security_type_scheme_idx" to table: "http_security"
CREATE INDEX CONCURRENTLY "http_security_type_scheme_idx" ON "http_security" ("type", "scheme");

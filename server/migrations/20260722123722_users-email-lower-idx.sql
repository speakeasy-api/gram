-- atlas:txmode none

-- Create index "users_email_lower_idx" to table: "users"
CREATE INDEX CONCURRENTLY "users_email_lower_idx" ON "users" ((lower(email)));

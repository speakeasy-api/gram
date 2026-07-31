-- atlas:txmode none

-- Create index "directory_users_organization_id_lower_email_idx" to table: "directory_users"
CREATE INDEX CONCURRENTLY "directory_users_organization_id_lower_email_idx" ON "directory_users" ("organization_id", (lower(email))) WHERE ((deleted IS FALSE) AND (workos_deleted IS FALSE));

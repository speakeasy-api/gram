-- atlas:txmode none

-- Create index "device_agent_syncs_organization_id_lower_email_idx" to table: "device_agent_syncs"
CREATE INDEX CONCURRENTLY "device_agent_syncs_organization_id_lower_email_idx" ON "device_agent_syncs" ("organization_id", (lower(email)));

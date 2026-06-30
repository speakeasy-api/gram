-- atlas:txmode none

-- Create index "device_owners_organization_id_idx" to table: "device_owners"
CREATE INDEX CONCURRENTLY "device_owners_organization_id_idx" ON "device_owners" ("organization_id");

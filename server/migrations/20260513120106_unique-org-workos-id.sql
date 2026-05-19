-- atlas:txmode none

-- Create index "organization_metadata_workos_id_key" to table: "organization_metadata"
CREATE UNIQUE INDEX CONCURRENTLY "organization_metadata_workos_id_key" ON "organization_metadata" ("workos_id");

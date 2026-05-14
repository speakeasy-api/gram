-- atlas:txmode none

-- Create index "organization_metadata_slug_key" to table: "organization_metadata"
CREATE UNIQUE INDEX CONCURRENTLY "organization_metadata_slug_key" ON "organization_metadata" ("slug");

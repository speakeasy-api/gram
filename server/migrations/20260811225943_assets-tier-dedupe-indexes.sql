-- atlas:txmode none

-- Create index "assets_organization_id_sha256_key" to table: "assets"
CREATE UNIQUE INDEX CONCURRENTLY "assets_organization_id_sha256_key" ON "assets" ("organization_id", "sha256") WHERE ((project_id IS NULL) AND (organization_id IS NOT NULL));
-- Create index "assets_platform_sha256_key" to table: "assets"
CREATE UNIQUE INDEX CONCURRENTLY "assets_platform_sha256_key" ON "assets" ("sha256") WHERE ((project_id IS NULL) AND (organization_id IS NULL));

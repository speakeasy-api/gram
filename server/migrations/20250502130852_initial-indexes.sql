-- atlas:txmode none

-- Create index "api_keys_organization_id_name_key" to table: "api_keys"
CREATE UNIQUE INDEX CONCURRENTLY "api_keys_organization_id_name_key" ON "api_keys" ("organization_id", "name") WHERE (deleted IS FALSE);
-- Create index "environments_project_id_slug_key" to table: "environments"
CREATE UNIQUE INDEX CONCURRENTLY "environments_project_id_slug_key" ON "environments" ("project_id", "slug") WHERE (deleted IS FALSE);
-- Create index "http_tool_definitions_name_idx" to table: "http_tool_definitions"
CREATE INDEX CONCURRENTLY "http_tool_definitions_name_idx" ON "http_tool_definitions" ("name");
-- Create index "package_versions_package_id_semver_key" to table: "package_versions"
CREATE UNIQUE INDEX CONCURRENTLY "package_versions_package_id_semver_key" ON "package_versions" ("package_id" DESC, "major" DESC, "minor" DESC, "patch" DESC, "prerelease", "build") WHERE (deleted IS FALSE);
-- Create index "packages_name_idx" to table: "packages"
CREATE INDEX CONCURRENTLY "packages_name_idx" ON "packages" ("name");
-- Create index "packages_organization_id_name_key" to table: "packages"
CREATE UNIQUE INDEX CONCURRENTLY "packages_organization_id_name_key" ON "packages" ("organization_id", "name") WHERE (deleted IS FALSE);
-- Create index "packages_project_id_key" to table: "packages"
CREATE UNIQUE INDEX CONCURRENTLY "packages_project_id_key" ON "packages" ("project_id") WHERE (deleted IS FALSE);
-- Create index "projects_organization_id_slug_key" to table: "projects"
CREATE UNIQUE INDEX CONCURRENTLY "projects_organization_id_slug_key" ON "projects" ("organization_id", "slug") WHERE (deleted IS FALSE);
-- Create index "toolsets_project_id_slug_key" to table: "toolsets"
CREATE UNIQUE INDEX CONCURRENTLY "toolsets_project_id_slug_key" ON "toolsets" ("project_id", "slug") WHERE (deleted IS FALSE);

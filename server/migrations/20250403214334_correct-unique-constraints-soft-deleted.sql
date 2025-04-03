-- Modify "api_keys" table
ALTER TABLE "api_keys" DROP CONSTRAINT "api_keys_organization_id_name_key";
-- Create index "api_keys_organization_id_name_key" to table: "api_keys"
CREATE UNIQUE INDEX "api_keys_organization_id_name_key" ON "api_keys" ("organization_id", "name") WHERE (deleted IS FALSE);
-- Modify "environments" table
ALTER TABLE "environments" DROP CONSTRAINT "environments_project_id_slug_key";
-- Create index "environments_project_id_slug_key" to table: "environments"
CREATE UNIQUE INDEX "environments_project_id_slug_key" ON "environments" ("project_id", "slug") WHERE (deleted IS FALSE);
-- Modify "projects" table
ALTER TABLE "projects" DROP CONSTRAINT "projects_organization_id_slug_key";
-- Create index "projects_organization_id_slug_key" to table: "projects"
CREATE UNIQUE INDEX "projects_organization_id_slug_key" ON "projects" ("organization_id", "slug") WHERE (deleted IS FALSE);
-- Modify "toolsets" table
ALTER TABLE "toolsets" DROP CONSTRAINT "toolsets_project_id_slug_key";
-- Create index "toolsets_project_id_slug_key" to table: "toolsets"
CREATE UNIQUE INDEX "toolsets_project_id_slug_key" ON "toolsets" ("project_id", "slug") WHERE (deleted IS FALSE);

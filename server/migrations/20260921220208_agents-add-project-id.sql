-- atlas:txmode none

-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "project_id" uuid NULL, ADD CONSTRAINT "agents_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
-- Create index "agents_project_id_idx" to table: "agents"
CREATE INDEX CONCURRENTLY "agents_project_id_idx" ON "agents" ("project_id");
-- Validate after the index exists, under SHARE UPDATE EXCLUSIVE, so agent
-- reads and writes keep running. Every existing row has a NULL project_id,
-- which the composite key does not check, so this cannot fail.
ALTER TABLE "agents" VALIDATE CONSTRAINT "agents_organization_id_project_id_fkey";

-- atlas:txmode none

-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "project_id" uuid NULL, ADD CONSTRAINT "agents_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "agents_project_id_idx" to table: "agents"
CREATE INDEX CONCURRENTLY "agents_project_id_idx" ON "agents" ("project_id");

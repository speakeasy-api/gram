-- atlas:txmode none

-- Modify "workload_issuers" table
ALTER TABLE "workload_issuers" ADD COLUMN "allow_prefix_admission" boolean NOT NULL DEFAULT false;
-- Modify "workload_identity_admissions" table
ALTER TABLE "workload_identity_admissions" ADD COLUMN "match_kind" text NOT NULL DEFAULT 'exact';
-- Modify "workload_agent_assignments" table
ALTER TABLE "workload_agent_assignments" ADD COLUMN "match_kind" text NOT NULL DEFAULT 'exact';
-- Drop index "workload_identity_admissions_organization_key" from table: "workload_identity_admissions"
DROP INDEX CONCURRENTLY "workload_identity_admissions_organization_key";
-- Create index "workload_identity_admissions_organization_key" to table: "workload_identity_admissions"
CREATE UNIQUE INDEX CONCURRENTLY "workload_identity_admissions_organization_key" ON "workload_identity_admissions" ("organization_id", "workload_issuer_id", "match_kind", "subject") WHERE ((deleted IS FALSE) AND (project_id IS NULL));
-- Drop index "workload_identity_admissions_project_key" from table: "workload_identity_admissions"
DROP INDEX CONCURRENTLY "workload_identity_admissions_project_key";
-- Create index "workload_identity_admissions_project_key" to table: "workload_identity_admissions"
CREATE UNIQUE INDEX CONCURRENTLY "workload_identity_admissions_project_key" ON "workload_identity_admissions" ("project_id", "workload_issuer_id", "match_kind", "subject") WHERE (deleted IS FALSE);
-- Drop index "workload_agent_assignments_workload_key" from table: "workload_agent_assignments"
DROP INDEX CONCURRENTLY "workload_agent_assignments_workload_key";
-- Create index "workload_agent_assignments_workload_key" to table: "workload_agent_assignments"
CREATE UNIQUE INDEX CONCURRENTLY "workload_agent_assignments_workload_key" ON "workload_agent_assignments" ("organization_id", "workload_issuer_id", "match_kind", "subject") WHERE (deleted IS FALSE);

-- atlas:txmode none

-- Modify "workload_issuers" table
ALTER TABLE "workload_issuers" ADD COLUMN "allow_wildcard_admission" boolean NOT NULL DEFAULT false;
-- Modify "workload_identity_admissions" table
ALTER TABLE "workload_identity_admissions" ADD COLUMN "match_kind" text NOT NULL DEFAULT 'exact';
-- Modify "workload_agent_assignments" table
ALTER TABLE "workload_agent_assignments" ADD COLUMN "match_kind" text NOT NULL DEFAULT 'exact';
-- Rebuild index "workload_identity_admissions_organization_key" on table: "workload_identity_admissions" to include match_kind.
-- Built under a swap name first so the uniqueness invariant is enforced by one
-- index or the other at every point: dropping first would leave a window with no
-- unique index, in which a duplicate could be written and then make the CREATE
-- below fail. Matches 20260818093227_platform-mcp-connectionless-surfaces.sql.
CREATE UNIQUE INDEX CONCURRENTLY "workload_identity_admissions_organization_key_swap" ON "workload_identity_admissions" ("organization_id", "workload_issuer_id", "match_kind", "subject") WHERE ((deleted IS FALSE) AND (project_id IS NULL));
DROP INDEX CONCURRENTLY "workload_identity_admissions_organization_key";
ALTER INDEX "workload_identity_admissions_organization_key_swap" RENAME TO "workload_identity_admissions_organization_key";
-- Rebuild index "workload_identity_admissions_project_key" on table: "workload_identity_admissions" to include match_kind.
-- Built under a swap name first so the uniqueness invariant is enforced by one
-- index or the other at every point: dropping first would leave a window with no
-- unique index, in which a duplicate could be written and then make the CREATE
-- below fail. Matches 20260818093227_platform-mcp-connectionless-surfaces.sql.
CREATE UNIQUE INDEX CONCURRENTLY "workload_identity_admissions_project_key_swap" ON "workload_identity_admissions" ("project_id", "workload_issuer_id", "match_kind", "subject") WHERE (deleted IS FALSE);
DROP INDEX CONCURRENTLY "workload_identity_admissions_project_key";
ALTER INDEX "workload_identity_admissions_project_key_swap" RENAME TO "workload_identity_admissions_project_key";
-- Rebuild index "workload_agent_assignments_workload_key" on table: "workload_agent_assignments" to include match_kind.
-- Built under a swap name first so the uniqueness invariant is enforced by one
-- index or the other at every point: dropping first would leave a window with no
-- unique index, in which a duplicate could be written and then make the CREATE
-- below fail. Matches 20260818093227_platform-mcp-connectionless-surfaces.sql.
CREATE UNIQUE INDEX CONCURRENTLY "workload_agent_assignments_workload_key_swap" ON "workload_agent_assignments" ("organization_id", "workload_issuer_id", "match_kind", "subject") WHERE (deleted IS FALSE);
DROP INDEX CONCURRENTLY "workload_agent_assignments_workload_key";
ALTER INDEX "workload_agent_assignments_workload_key_swap" RENAME TO "workload_agent_assignments_workload_key";

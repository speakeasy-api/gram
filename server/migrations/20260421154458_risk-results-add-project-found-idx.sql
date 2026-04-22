-- atlas:txmode none

-- Speeds up ListRiskResultsByProjectFound and CountAllFindings queries which
-- filter by project_id and found IS TRUE, ordered by created_at DESC.
CREATE INDEX CONCURRENTLY "risk_results_project_found_idx" ON "risk_results" ("project_id", "created_at" DESC) WHERE (found IS TRUE);

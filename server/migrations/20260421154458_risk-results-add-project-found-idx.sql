-- atlas:txmode none

-- Create index "risk_results_project_found_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_project_found_idx" ON "risk_results" ("project_id", "created_at" DESC) WHERE (found IS TRUE);

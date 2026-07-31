-- atlas:txmode none

-- Create index "risk_results_project_false_positive_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_project_false_positive_idx" ON "risk_results" ("project_id", "false_positive_at" DESC, "id" DESC) WHERE (false_positive_at IS NOT NULL);

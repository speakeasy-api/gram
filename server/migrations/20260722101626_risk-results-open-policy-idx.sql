-- atlas:txmode none

-- Create index "risk_results_project_open_policy_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_project_open_policy_idx" ON "risk_results" ("project_id", "risk_policy_id") WHERE ((found IS TRUE) AND (excluded_at IS NULL) AND (false_positive_at IS NULL));

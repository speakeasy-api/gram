-- atlas:txmode none

-- Drop index "risk_results_policy_match_idx" from table: "risk_results"
DROP INDEX CONCURRENTLY "risk_results_policy_match_idx";
-- Create index "risk_results_policy_match_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_policy_match_idx" ON "risk_results" ("project_id", "risk_policy_id", "rule_id");

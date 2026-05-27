-- atlas:txmode none

-- Create index "risk_results_message_policy_coverage_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_message_policy_coverage_idx" ON "risk_results" ("chat_message_id", "project_id", "risk_policy_id", "risk_policy_version");

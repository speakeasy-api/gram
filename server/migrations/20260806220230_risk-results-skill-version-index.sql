-- atlas:txmode none

-- Create index "risk_results_skill_version_id_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_skill_version_id_idx" ON "risk_results" ("skill_version_id", "risk_policy_id") WHERE (skill_version_id IS NOT NULL);

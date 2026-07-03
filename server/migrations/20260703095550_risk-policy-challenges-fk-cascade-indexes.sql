-- atlas:txmode none

-- Create index "risk_policy_challenges_organization_id_idx" to table: "risk_policy_challenges"
CREATE INDEX CONCURRENTLY IF NOT EXISTS "risk_policy_challenges_organization_id_idx" ON "risk_policy_challenges" ("organization_id");
-- Create index "risk_policy_challenges_project_id_idx" to table: "risk_policy_challenges"
CREATE INDEX CONCURRENTLY "risk_policy_challenges_project_id_idx" ON "risk_policy_challenges" ("project_id");
-- Create index "risk_policy_challenges_risk_policy_id_idx" to table: "risk_policy_challenges"
CREATE INDEX CONCURRENTLY "risk_policy_challenges_risk_policy_id_idx" ON "risk_policy_challenges" ("risk_policy_id");

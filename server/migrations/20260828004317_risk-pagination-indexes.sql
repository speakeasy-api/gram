-- atlas:txmode none

-- Create index "risk_exclusions_project_id_created_at_id_idx" to table: "risk_exclusions"
CREATE INDEX CONCURRENTLY "risk_exclusions_project_id_created_at_id_idx" ON "risk_exclusions" ("project_id", "created_at" DESC, "id" DESC) WHERE (deleted IS FALSE);
-- Create index "risk_exclusions_project_id_risk_policy_id_created_at_id_idx" to table: "risk_exclusions"
CREATE INDEX CONCURRENTLY "risk_exclusions_project_id_risk_policy_id_created_at_id_idx" ON "risk_exclusions" ("project_id", "risk_policy_id", "created_at" DESC, "id" DESC) WHERE (deleted IS FALSE);
-- Create index "risk_policies_project_id_created_at_id_idx" to table: "risk_policies"
CREATE INDEX CONCURRENTLY "risk_policies_project_id_created_at_id_idx" ON "risk_policies" ("project_id", "created_at" DESC, "id" DESC) WHERE (deleted IS FALSE);

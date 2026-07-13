-- atlas:txmode none

-- Drop index "risk_policy_challenges_active_ack_idx" from table: "risk_policy_challenges"
DROP INDEX CONCURRENTLY "risk_policy_challenges_active_ack_idx";
-- Drop index "risk_policy_challenges_current_key" from table: "risk_policy_challenges"
DROP INDEX CONCURRENTLY "risk_policy_challenges_current_key";
-- Modify "risk_policy_challenges" table
ALTER TABLE "risk_policy_challenges" ADD COLUMN "call_fingerprint" text NULL;
-- Create index "risk_policy_challenges_active_ack_idx" to table: "risk_policy_challenges"
CREATE INDEX CONCURRENTLY "risk_policy_challenges_active_ack_idx" ON "risk_policy_challenges" ("project_id", "user_id", "risk_policy_id", "tool_name", "call_fingerprint", "expires_at") WHERE ((deleted IS FALSE) AND (status = 'acknowledged'::text));
-- Create index "risk_policy_challenges_current_key" to table: "risk_policy_challenges"
CREATE UNIQUE INDEX CONCURRENTLY "risk_policy_challenges_current_key" ON "risk_policy_challenges" ("project_id", "user_id", "risk_policy_id", "tool_name", "call_fingerprint") NULLS NOT DISTINCT WHERE (deleted IS FALSE);

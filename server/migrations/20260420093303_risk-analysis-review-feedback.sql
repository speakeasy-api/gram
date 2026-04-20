-- atlas:txmode none

-- Modify "risk_policies" table
ALTER TABLE "risk_policies" DROP CONSTRAINT "risk_policies_project_id_fkey";
-- Drop index "risk_results_policy_version_message_idx" from table: "risk_results"
DROP INDEX CONCURRENTLY "risk_results_policy_version_message_idx";
-- Modify "risk_results" table
ALTER TABLE "risk_results" DROP CONSTRAINT "risk_results_chat_message_id_fkey", DROP CONSTRAINT "risk_results_project_id_fkey", DROP CONSTRAINT "risk_results_risk_policy_id_fkey", DROP COLUMN "policy_version", ADD COLUMN "organization_id" text NOT NULL, ADD COLUMN "risk_policy_version" bigint NOT NULL;
-- Create index "risk_results_policy_version_message_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_policy_version_message_idx" ON "risk_results" ("risk_policy_id", "risk_policy_version", "chat_message_id");

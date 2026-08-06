-- atlas:txmode none

-- Modify "risk_results" table
ALTER TABLE "risk_results" DROP CONSTRAINT "risk_results_anchor_check", ADD CONSTRAINT "risk_results_anchor_check" CHECK (num_nonnulls(chat_message_id, chat_content_part_id, skill_version_id) = 1), ADD COLUMN "skill_version_id" uuid NULL, ADD CONSTRAINT "risk_results_skill_version_id_fkey" FOREIGN KEY ("skill_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "risk_results_skill_version_id_risk_policy_id_key" to table: "risk_results"
CREATE UNIQUE INDEX CONCURRENTLY "risk_results_skill_version_id_risk_policy_id_key" ON "risk_results" ("skill_version_id", "risk_policy_id") WHERE (skill_version_id IS NOT NULL);

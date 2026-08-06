-- atlas:txmode none

-- Modify "risk_results" table
ALTER TABLE "risk_results" DROP CONSTRAINT "risk_results_anchor_check", ADD CONSTRAINT "risk_results_anchor_check" CHECK (num_nonnulls(chat_message_id, chat_content_part_id, skill_version_id) = 1) NOT VALID, ADD COLUMN "skill_version_id" uuid NULL, ADD CONSTRAINT "risk_results_skill_version_id_fkey" FOREIGN KEY ("skill_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "risk_results" VALIDATE CONSTRAINT "risk_results_anchor_check";
ALTER TABLE "risk_results" VALIDATE CONSTRAINT "risk_results_skill_version_id_fkey";

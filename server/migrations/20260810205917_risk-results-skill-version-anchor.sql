-- atlas:txmode none

-- Modify "risk_results" table
-- NOT VALID two-step: risk_results is append-heavy, so the plain form's scan
-- under ACCESS EXCLUSIVE would block reads and writes. VALIDATE cannot fail:
-- skill_version_id is NULL on every existing row, so the FK is vacuous and
-- the new check reduces to the old one it replaces. The index is built before
-- the FK VALIDATE so the validation scan hits the (empty) partial index
-- instead of seq-scanning the table.
ALTER TABLE "risk_results" DROP CONSTRAINT "risk_results_anchor_check", ADD CONSTRAINT "risk_results_anchor_check" CHECK (num_nonnulls(chat_message_id, chat_content_part_id, skill_version_id) = 1) NOT VALID, ADD COLUMN "skill_version_id" uuid NULL, ADD CONSTRAINT "risk_results_skill_version_id_fkey" FOREIGN KEY ("skill_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
-- Create index "risk_results_skill_version_policy_version_key" to table: "risk_results"
CREATE UNIQUE INDEX CONCURRENTLY "risk_results_skill_version_policy_version_key" ON "risk_results" ("skill_version_id", "risk_policy_id", "risk_policy_version") WHERE (skill_version_id IS NOT NULL);
ALTER TABLE "risk_results" VALIDATE CONSTRAINT "risk_results_anchor_check";
ALTER TABLE "risk_results" VALIDATE CONSTRAINT "risk_results_skill_version_id_fkey";

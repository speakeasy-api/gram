-- atlas:txmode none

-- Modify "risk_results" table
-- Hand-edited into the NOT VALID two-step: risk_results is the append-heavy
-- findings table, so the plain form's full scan under ACCESS EXCLUSIVE would
-- block reads and writes on it for the duration. Both constraints hold over
-- every existing row without a scan - skill_version_id is NULL everywhere, so
-- the FK is vacuous and the new check reduces to the old one it replaces - so
-- VALIDATE cannot fail. Keep the ADDs in one ALTER TABLE: splitting the
-- check's DROP from its ADD would leave the table briefly unconstrained.
ALTER TABLE "risk_results" DROP CONSTRAINT "risk_results_anchor_check", ADD CONSTRAINT "risk_results_anchor_check" CHECK (num_nonnulls(chat_message_id, chat_content_part_id, skill_version_id) = 1) NOT VALID, ADD COLUMN "skill_version_id" uuid NULL, ADD CONSTRAINT "risk_results_skill_version_id_fkey" FOREIGN KEY ("skill_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "risk_results" VALIDATE CONSTRAINT "risk_results_anchor_check";
ALTER TABLE "risk_results" VALIDATE CONSTRAINT "risk_results_skill_version_id_fkey";
-- Create index "risk_results_skill_version_policy_version_key" to table: "risk_results"
CREATE UNIQUE INDEX CONCURRENTLY "risk_results_skill_version_policy_version_key" ON "risk_results" ("skill_version_id", "risk_policy_id", "risk_policy_version") WHERE (skill_version_id IS NOT NULL);

-- Modify "skill_observations" table
ALTER TABLE "skill_observations" DROP CONSTRAINT "skill_observations_skill_version_id_fkey", ADD CONSTRAINT "skill_observations_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION;

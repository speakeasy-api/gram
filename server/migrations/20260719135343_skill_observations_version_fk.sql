-- Modify "skill_observations" table
ALTER TABLE "skill_observations" DROP CONSTRAINT "skill_observations_skill_id_skill_version_id_fkey", ADD CONSTRAINT "skill_observations_skill_version_id_fkey" FOREIGN KEY ("skill_version_id") REFERENCES "skill_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;

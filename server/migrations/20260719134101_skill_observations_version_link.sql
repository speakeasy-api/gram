-- atlas:txmode none

-- Modify "skill_observations" table
ALTER TABLE "skill_observations" ADD CONSTRAINT "skill_observations_skill_id_skill_version_id_check" CHECK ((skill_version_id IS NULL) OR (skill_id IS NOT NULL)), ADD COLUMN "skill_version_id" uuid NULL, ADD CONSTRAINT "skill_observations_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "skill_observations_project_id_skill_version_id_seen_at_idx" to table: "skill_observations"
CREATE INDEX CONCURRENTLY "skill_observations_project_id_skill_version_id_seen_at_idx" ON "skill_observations" ("project_id", "skill_version_id", "seen_at" DESC) WHERE (skill_version_id IS NOT NULL);

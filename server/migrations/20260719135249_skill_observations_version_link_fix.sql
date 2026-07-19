-- atlas:txmode none

-- Create index "skill_observations_skill_id_skill_version_id_idx" to table: "skill_observations"
CREATE INDEX CONCURRENTLY "skill_observations_skill_id_skill_version_id_idx" ON "skill_observations" ("skill_id", "skill_version_id") WHERE (skill_version_id IS NOT NULL);

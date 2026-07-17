-- atlas:txmode none

-- Create index "skill_versions_skill_id_created_at_id_idx" to table: "skill_versions"
CREATE INDEX CONCURRENTLY "skill_versions_skill_id_created_at_id_idx" ON "skill_versions" ("skill_id", "created_at" DESC, "id" DESC);

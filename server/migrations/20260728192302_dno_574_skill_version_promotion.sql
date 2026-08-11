-- atlas:txmode none

-- Create index "skill_feedback_project_id_skill_id_created_at_id_idx" to table: "skill_feedback"
CREATE INDEX CONCURRENTLY "skill_feedback_project_id_skill_id_created_at_id_idx" ON "skill_feedback" ("project_id", "skill_id", "created_at" DESC, "id" DESC) WHERE (skill_id IS NOT NULL);
-- Modify "skill_versions" table
ALTER TABLE "skill_versions" ADD COLUMN "promoted_at" timestamptz NULL;
-- Create index "skill_versions_skill_id_effective_at_id_idx" to table: "skill_versions"
CREATE INDEX CONCURRENTLY "skill_versions_skill_id_effective_at_id_idx" ON "skill_versions" ("skill_id", (COALESCE(promoted_at, created_at)) DESC, "id" DESC);

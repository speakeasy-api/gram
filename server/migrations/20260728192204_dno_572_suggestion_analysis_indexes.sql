-- atlas:txmode none

-- Create index "skill_efficacy_evaluations_suggestion_scored_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX CONCURRENTLY "skill_efficacy_evaluations_suggestion_scored_idx" ON "skill_efficacy_evaluations" ("project_id", "skill_id", "skill_version_id", "scored_at" DESC, "id" DESC) WHERE (state = 'scored'::text);
-- Create index "skills_suggestion_sweep_idx" to table: "skills"
CREATE INDEX CONCURRENTLY "skills_suggestion_sweep_idx" ON "skills" ("project_id", "last_seen_at" DESC, "id" DESC) WHERE (archived_at IS NULL);

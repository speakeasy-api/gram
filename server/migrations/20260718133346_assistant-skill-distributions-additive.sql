-- atlas:txmode none

-- Modify "assistant_threads" table
ALTER TABLE "assistant_threads" ADD COLUMN "skill_set_snapshot" jsonb NULL;
-- Create index "assistants_project_id_id_key" to table: "assistants"
CREATE UNIQUE INDEX CONCURRENTLY "assistants_project_id_id_key" ON "assistants" ("project_id", "id");
-- Modify "skill_distributions" table
ALTER TABLE "skill_distributions" ADD COLUMN "assistant_id" uuid NULL, ADD CONSTRAINT "skill_distributions_project_id_assistant_id_fkey" FOREIGN KEY ("project_id", "assistant_id") REFERENCES "assistants" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION;
-- Create index "skill_distributions_active_target_key" to table: "skill_distributions"
CREATE UNIQUE INDEX CONCURRENTLY "skill_distributions_active_target_key" ON "skill_distributions" ("project_id", "skill_id", "channel", "plugin_id", "assistant_id") NULLS NOT DISTINCT WHERE (revoked_at IS NULL);
-- Create index "skill_distributions_assistant_id_idx" to table: "skill_distributions"
CREATE INDEX CONCURRENTLY "skill_distributions_assistant_id_idx" ON "skill_distributions" ("assistant_id");

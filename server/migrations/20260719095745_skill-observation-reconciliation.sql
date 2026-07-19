-- atlas:txmode none

-- Modify "skills" table
ALTER TABLE "skills" ADD COLUMN "first_seen_at" timestamptz NULL, ADD COLUMN "last_seen_at" timestamptz NULL, ADD COLUMN "seen_count" bigint NULL DEFAULT 0;
-- Modify "skill_observations" table
ALTER TABLE "skill_observations" ADD COLUMN "source" text NULL, ADD COLUMN "skill_id" uuid NULL, ADD COLUMN "reconciled_at" timestamptz NULL, ADD COLUMN "reconcile_error_code" text NULL, ADD CONSTRAINT "skill_observations_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION;
-- Create index "skill_observations_pending_reconciliation_idx" to table: "skill_observations"
CREATE INDEX CONCURRENTLY "skill_observations_pending_reconciliation_idx" ON "skill_observations" ("project_id", "seen_at", "id") WHERE (reconciled_at IS NULL);
-- Create index "skill_observations_project_id_skill_id_seen_at_idx" to table: "skill_observations"
CREATE INDEX CONCURRENTLY "skill_observations_project_id_skill_id_seen_at_idx" ON "skill_observations" ("project_id", "skill_id", "seen_at" DESC) WHERE (skill_id IS NOT NULL);

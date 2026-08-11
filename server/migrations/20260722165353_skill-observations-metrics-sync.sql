-- atlas:txmode none

-- Modify "skill_observations" table
ALTER TABLE "skill_observations" ADD COLUMN "metrics_synced_at" timestamptz NULL;
-- Create index "skill_observations_pending_metrics_sync_idx" to table: "skill_observations"
CREATE INDEX CONCURRENTLY "skill_observations_pending_metrics_sync_idx" ON "skill_observations" ("project_id", "seen_at", "id") WHERE ((reconciled_at IS NOT NULL) AND (metrics_synced_at IS NULL) AND (session_id IS NOT NULL) AND (skill_version_id IS NOT NULL));

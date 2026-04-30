-- atlas:txmode none

-- Drop index "workos_user_syncs_last_event_id_desc_idx" from table: "workos_user_syncs"
DROP INDEX CONCURRENTLY "workos_user_syncs_last_event_id_desc_idx";
-- Modify "workos_user_syncs" table
ALTER TABLE "workos_user_syncs" ADD CONSTRAINT "workos_user_syncs_singleton_check" CHECK (id = 1), ADD COLUMN "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp();

-- atlas:txmode none

-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "workos_created_at" timestamptz NULL, ADD COLUMN "workos_updated_at" timestamptz NULL, ADD COLUMN "workos_deleted_at" timestamptz NULL, ADD COLUMN "workos_deleted" boolean NOT NULL GENERATED ALWAYS AS (workos_deleted_at IS NOT NULL) STORED;
-- Modify "workos_user_syncs" table
ALTER TABLE "workos_user_syncs" DROP CONSTRAINT "workos_user_syncs_singleton_check", ADD COLUMN "workos_user_id" text NULL;
-- Create index "workos_user_syncs_workos_user_id_key" to table: "workos_user_syncs"
CREATE UNIQUE INDEX CONCURRENTLY "workos_user_syncs_workos_user_id_key" ON "workos_user_syncs" ("workos_user_id") WHERE (workos_user_id IS NOT NULL);

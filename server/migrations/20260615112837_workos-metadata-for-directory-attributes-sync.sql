-- Modify "directory_groups" table
ALTER TABLE "directory_groups" ADD COLUMN "workos_created_at" timestamptz NOT NULL, ADD COLUMN "workos_updated_at" timestamptz NOT NULL, ADD COLUMN "workos_deleted_at" timestamptz NULL, ADD COLUMN "workos_deleted" boolean NOT NULL GENERATED ALWAYS AS (workos_deleted_at IS NOT NULL) STORED, ADD COLUMN "workos_last_event_id" text NULL;
-- Modify "directory_users" table
ALTER TABLE "directory_users" ADD COLUMN "workos_created_at" timestamptz NOT NULL, ADD COLUMN "workos_updated_at" timestamptz NOT NULL, ADD COLUMN "workos_deleted_at" timestamptz NULL, ADD COLUMN "workos_deleted" boolean NOT NULL GENERATED ALWAYS AS (workos_deleted_at IS NOT NULL) STORED, ADD COLUMN "workos_last_event_id" text NULL;

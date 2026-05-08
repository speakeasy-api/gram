-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "workos_updated_at" timestamptz NULL, ADD COLUMN "workos_last_event_id" text NULL;
-- Modify "organization_user_relationships" table
ALTER TABLE "organization_user_relationships" ADD COLUMN "workos_updated_at" timestamptz NULL, ADD COLUMN "workos_last_event_id" text NULL;

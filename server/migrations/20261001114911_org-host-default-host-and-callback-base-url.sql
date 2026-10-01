-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "default_host" text NULL;
-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "callback_base_url" text NULL;

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "legacy_callback_url" boolean NOT NULL DEFAULT false;

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "registration_endpoint" text NULL, ADD COLUMN "upstream_rejected_at" timestamptz NULL;

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "scope" text[] NULL, ADD COLUMN "audience" text NULL;

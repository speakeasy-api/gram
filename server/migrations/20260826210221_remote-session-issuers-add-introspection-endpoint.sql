-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "introspection_endpoint" text NULL;

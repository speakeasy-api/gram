-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "scope_override" text[] NULL, ADD COLUMN "resource_indicator_supported" boolean NULL;

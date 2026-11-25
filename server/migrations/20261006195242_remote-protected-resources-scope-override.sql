-- Modify "remote_protected_resources" table
ALTER TABLE "remote_protected_resources" ADD COLUMN "scope_override" text[] NULL;
-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "omit_scope_fallback" boolean NULL;

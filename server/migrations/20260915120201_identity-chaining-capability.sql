-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "grant_types" text[] NULL;
-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "authorization_grant_profiles_supported" text[] NOT NULL DEFAULT ARRAY[]::text[];

-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "code_challenge_methods_supported" text[] NULL;

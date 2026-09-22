-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ADD COLUMN "use_authentication_host" boolean NOT NULL DEFAULT false;

-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "jwks_last_error" text NULL, ADD COLUMN "jwks_last_error_at" timestamptz NULL;

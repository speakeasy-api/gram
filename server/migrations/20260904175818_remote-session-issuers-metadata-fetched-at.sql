-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "metadata_fetched_at" timestamptz NULL, ADD COLUMN "metadata_last_error" text NULL, ADD COLUMN "metadata_last_error_at" timestamptz NULL;

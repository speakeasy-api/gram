-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "add_standard_scopes" boolean NULL;
-- Set comment to column: "add_standard_scopes" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."add_standard_scopes" IS '@access: confidential';

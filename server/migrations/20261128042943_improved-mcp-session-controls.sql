-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "session_inactivity_timeout" interval NULL, ADD COLUMN "session_maximum_duration" interval NULL;
-- Set comment to column: "session_inactivity_timeout" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."session_inactivity_timeout" IS '@access: confidential';
-- Set comment to column: "session_maximum_duration" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."session_maximum_duration" IS '@access: confidential';
-- Modify "remote_sessions" table
ALTER TABLE "remote_sessions" ADD COLUMN "authorized_at" timestamptz NULL;
-- Set comment to column: "authorized_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."authorized_at" IS '@access: confidential';
-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ADD COLUMN "session_inactivity_timeout" interval NULL;
-- Set comment to column: "session_inactivity_timeout" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."session_inactivity_timeout" IS '@access: confidential';
-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "session_inactivity_timeout" interval NULL, ADD COLUMN "session_absolute_expires_at" timestamptz NULL;
-- Set comment to column: "session_inactivity_timeout" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."session_inactivity_timeout" IS '@access: confidential';
-- Set comment to column: "session_absolute_expires_at" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."session_absolute_expires_at" IS '@access: confidential';

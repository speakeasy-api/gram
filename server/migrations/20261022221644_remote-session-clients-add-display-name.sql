-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "display_name" text NULL;
-- Set comment to column: "display_name" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."display_name" IS '@access: opaque-confidential';

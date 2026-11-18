-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "forward_client_name" boolean NOT NULL DEFAULT false;
-- Set comment to column: "forward_client_name" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."forward_client_name" IS '@access: confidential';

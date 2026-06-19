-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "token_endpoint_auth_method" text NULL;

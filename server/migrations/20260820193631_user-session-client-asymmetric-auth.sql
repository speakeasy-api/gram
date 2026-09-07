-- atlas:txmode none

-- Modify "user_session_clients" table
ALTER TABLE "user_session_clients" ADD CONSTRAINT "user_session_clients_client_jwks_source_check" CHECK (num_nonnulls(client_jwks, client_jwks_uri) <= 1) NOT VALID, ADD COLUMN "token_endpoint_auth_method" text NULL, ADD COLUMN "client_jwks" jsonb NULL, ADD COLUMN "client_jwks_uri" text NULL;
ALTER TABLE "user_session_clients" VALIDATE CONSTRAINT "user_session_clients_client_jwks_source_check";

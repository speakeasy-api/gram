-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD CONSTRAINT "remote_session_clients_client_id_metadata_uri_check" CHECK ((client_id_metadata_uri IS NULL) OR ((client_id_metadata_uri <> ''::text) AND (client_secret_encrypted IS NULL) AND (client_id = client_id_metadata_uri))), ADD COLUMN "client_id_metadata_uri" text NULL;
-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "client_id_metadata_document_supported" boolean NOT NULL DEFAULT false;

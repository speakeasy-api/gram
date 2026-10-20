-- atlas:txmode none

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD CONSTRAINT "remote_session_clients_credential_owner_check" CHECK ((credential_owner <> 'self'::text) OR (((project_id IS NOT NULL) OR (organization_id IS NOT NULL)) AND (client_id_metadata_uri IS NULL) AND (legacy_callback_url IS FALSE) AND (token_endpoint_auth_method IS NOT NULL) AND (token_endpoint_auth_method <> ALL (ARRAY[''::text, 'none'::text])))) NOT VALID, ADD COLUMN "credential_owner" text NOT NULL DEFAULT 'subject';
-- Validate separately so the scan does not hold the ADD COLUMN lock.
ALTER TABLE "remote_session_clients" VALIDATE CONSTRAINT "remote_session_clients_credential_owner_check";

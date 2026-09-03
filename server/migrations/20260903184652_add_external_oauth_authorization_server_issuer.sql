-- atlas:txmode none

-- Modify "external_oauth_server_metadata" table
ALTER TABLE "external_oauth_server_metadata" ADD CONSTRAINT "external_oauth_server_metadata_authorization_server_issuer_chec" CHECK ((authorization_server_issuer IS NULL) OR ((authorization_server_issuer <> ''::text) AND (char_length(authorization_server_issuer) <= 500))) NOT VALID, ADD COLUMN "authorization_server_issuer" text NULL;
ALTER TABLE "external_oauth_server_metadata" VALIDATE CONSTRAINT "external_oauth_server_metadata_authorization_server_issuer_chec";

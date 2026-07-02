-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD CONSTRAINT "remote_session_clients_resource_check" CHECK ((resource IS NULL) OR (btrim(resource) <> ''::text)), ADD COLUMN "resource" text NULL;

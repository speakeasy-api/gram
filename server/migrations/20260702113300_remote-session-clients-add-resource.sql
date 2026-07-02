-- atlas:txmode none

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "resource" text NULL;
-- Add the CHECK as NOT VALID then validate separately: validation only takes
-- a SHARE UPDATE EXCLUSIVE lock, avoiding the full-table scan under ACCESS
-- EXCLUSIVE that an inline CHECK would require (PG305).
ALTER TABLE "remote_session_clients" ADD CONSTRAINT "remote_session_clients_resource_check" CHECK ((resource IS NULL) OR (btrim(resource) <> ''::text)) NOT VALID;
ALTER TABLE "remote_session_clients" VALIDATE CONSTRAINT "remote_session_clients_resource_check";

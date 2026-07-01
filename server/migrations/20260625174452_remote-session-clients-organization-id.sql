-- atlas:txmode none

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "organization_id" text NULL, ADD CONSTRAINT "remote_session_clients_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "remote_session_clients_organization_id_idx" to table: "remote_session_clients"
CREATE INDEX CONCURRENTLY "remote_session_clients_organization_id_idx" ON "remote_session_clients" ("organization_id") WHERE (deleted IS FALSE);

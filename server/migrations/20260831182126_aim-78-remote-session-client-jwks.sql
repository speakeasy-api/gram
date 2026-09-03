-- atlas:txmode none

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD CONSTRAINT "remote_session_clients_json_web_key_set_id_check" CHECK ((json_web_key_set_id IS NULL) OR (organization_id IS NOT NULL)) NOT VALID, ADD COLUMN "json_web_key_set_id" uuid NULL, ADD CONSTRAINT "remote_session_clients_json_web_key_set_tenant_fkey" FOREIGN KEY ("organization_id", "json_web_key_set_id") REFERENCES "json_web_key_sets" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID;
ALTER TABLE "remote_session_clients" VALIDATE CONSTRAINT "remote_session_clients_json_web_key_set_id_check";
ALTER TABLE "remote_session_clients" VALIDATE CONSTRAINT "remote_session_clients_json_web_key_set_tenant_fkey";
-- Create index "remote_session_clients_json_web_key_set_idx" to table: "remote_session_clients"
CREATE INDEX CONCURRENTLY "remote_session_clients_json_web_key_set_idx" ON "remote_session_clients" ("organization_id", "json_web_key_set_id");

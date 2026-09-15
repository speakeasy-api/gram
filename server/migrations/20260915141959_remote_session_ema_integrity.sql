-- atlas:txmode none

-- Create trigger "remote_session_ema_project_update_guard"
CREATE TRIGGER "remote_session_ema_project_update_guard" BEFORE UPDATE ON "projects" FOR EACH ROW WHEN ((old.deleted_at IS DISTINCT FROM new.deleted_at) OR (old.organization_id IS DISTINCT FROM new.organization_id)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Modify "remote_session_ema_client_update_guard" trigger
CREATE OR REPLACE TRIGGER "remote_session_ema_client_update_guard" BEFORE UPDATE ON "remote_session_clients" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.remote_session_issuer_id IS DISTINCT FROM new.remote_session_issuer_id) OR (old.deleted_at IS DISTINCT FROM new.deleted_at) OR (old.audience IS DISTINCT FROM new.audience) OR (old.client_id IS DISTINCT FROM new.client_id) OR (old.client_secret_encrypted IS DISTINCT FROM new.client_secret_encrypted) OR (old.token_endpoint_auth_method IS DISTINCT FROM new.token_endpoint_auth_method) OR (old.json_web_key_set_id IS DISTINCT FROM new.json_web_key_set_id) OR (old.scope IS DISTINCT FROM new.scope) OR (old.client_id_metadata_uri IS DISTINCT FROM new.client_id_metadata_uri) OR (old.token_endpoint_auth_audience_format IS DISTINCT FROM new.token_endpoint_auth_audience_format)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create index "remote_session_clients_id_issuer_key" to table: "remote_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_clients_id_issuer_key" ON "remote_session_clients" ("id", "remote_session_issuer_id");
-- Drop index "remote_session_ema_bindings_client_idx" from table: "remote_session_ema_bindings"
DROP INDEX CONCURRENTLY "remote_session_ema_bindings_client_idx";
-- Drop index "remote_session_ema_bindings_issuer_idx" from table: "remote_session_ema_bindings"
DROP INDEX CONCURRENTLY "remote_session_ema_bindings_issuer_idx";
-- Drop index "remote_session_ema_bindings_user_issuer_idx" from table: "remote_session_ema_bindings"
DROP INDEX CONCURRENTLY "remote_session_ema_bindings_user_issuer_idx";
-- Modify "remote_session_ema_bindings" table
ALTER TABLE "remote_session_ema_bindings" DROP CONSTRAINT "remote_session_ema_bindings_project_id_fkey", DROP CONSTRAINT "remote_session_ema_bindings_remote_session_client_id_fkey", ADD CONSTRAINT "remote_session_ema_bindings_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "remote_session_ema_bindings_remote_session_client_id_remot_fkey" FOREIGN KEY ("remote_session_client_id", "remote_session_issuer_id") REFERENCES "remote_session_clients" ("id", "remote_session_issuer_id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "remote_session_ema_bindings_client_idx" to table: "remote_session_ema_bindings"
CREATE INDEX CONCURRENTLY "remote_session_ema_bindings_client_idx" ON "remote_session_ema_bindings" ("remote_session_client_id");
-- Create index "remote_session_ema_bindings_issuer_idx" to table: "remote_session_ema_bindings"
CREATE INDEX CONCURRENTLY "remote_session_ema_bindings_issuer_idx" ON "remote_session_ema_bindings" ("remote_session_issuer_id");
-- Create index "remote_session_ema_bindings_user_issuer_idx" to table: "remote_session_ema_bindings"
CREATE INDEX CONCURRENTLY "remote_session_ema_bindings_user_issuer_idx" ON "remote_session_ema_bindings" ("user_session_issuer_id");

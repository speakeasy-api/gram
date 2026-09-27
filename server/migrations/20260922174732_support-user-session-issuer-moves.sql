-- atlas:txmode none

-- Modify "meta_mcp_servers" table
ALTER TABLE "meta_mcp_servers" DROP CONSTRAINT "meta_mcp_servers_project_id_user_session_issuer_id_fkey";
-- Modify "platform_mcp_catalog_registrations" table
ALTER TABLE "platform_mcp_catalog_registrations" DROP CONSTRAINT "platform_mcp_catalog_registrations_session_issuer_fkey";
-- Modify "principal_remote_session_bindings" table
ALTER TABLE "principal_remote_session_bindings" DROP CONSTRAINT "principal_remote_session_bindings_issuer_scope_fkey", DROP CONSTRAINT "principal_remote_session_bindings_session_fkey", ADD CONSTRAINT "principal_remote_session_bindings_issuer_scope_fkey" FOREIGN KEY ("user_session_issuer_id", "issuer_attachment_scope") REFERENCES "user_session_issuers" ("id", "attachment_scope") ON UPDATE CASCADE ON DELETE CASCADE NOT VALID, ADD CONSTRAINT "principal_remote_session_bindings_session_fkey" FOREIGN KEY ("remote_session_client_id", "user_session_issuer_id", "remote_session_id") REFERENCES "remote_sessions" ("remote_session_client_id", "user_session_issuer_id", "id") ON UPDATE CASCADE ON DELETE CASCADE NOT VALID;
ALTER TABLE "principal_remote_session_bindings" VALIDATE CONSTRAINT "principal_remote_session_bindings_issuer_scope_fkey";
ALTER TABLE "principal_remote_session_bindings" VALIDATE CONSTRAINT "principal_remote_session_bindings_session_fkey";

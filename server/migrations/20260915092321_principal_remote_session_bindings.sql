-- atlas:txmode none

-- Create "set_principal_remote_session_binding_scopes" function
CREATE FUNCTION "set_principal_remote_session_binding_scopes" () RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  SELECT attachment_scope INTO NEW.issuer_attachment_scope
  FROM user_session_issuers WHERE id = NEW.user_session_issuer_id;
  SELECT attachment_scope INTO NEW.client_attachment_scope
  FROM remote_session_clients WHERE id = NEW.remote_session_client_id;
  RETURN NEW;
END;
$$;
-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "attachment_scope" text NULL GENERATED ALWAYS AS (
CASE
    WHEN (project_id IS NOT NULL) THEN ('project:'::text || (project_id)::text)
    WHEN (organization_id IS NOT NULL) THEN ('organization:'::text || organization_id)
    ELSE 'global'::text
END) STORED;
-- Create index "remote_session_clients_attachment_scope_key" to table: "remote_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_clients_attachment_scope_key" ON "remote_session_clients" ("id", "attachment_scope");
-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ADD COLUMN "attachment_scope" text NULL GENERATED ALWAYS AS (
CASE
    WHEN (project_id IS NOT NULL) THEN ('project:'::text || (project_id)::text)
    WHEN (organization_id IS NOT NULL) THEN ('organization:'::text || organization_id)
    ELSE 'global'::text
END) STORED;
-- Create index "user_session_issuers_attachment_scope_key" to table: "user_session_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "user_session_issuers_attachment_scope_key" ON "user_session_issuers" ("id", "attachment_scope");
-- Modify "remote_sessions" table
ALTER TABLE "remote_sessions" ADD COLUMN "grant_generation" bigint NOT NULL DEFAULT 1;
-- Create index "remote_sessions_client_issuer_id_key" to table: "remote_sessions"
CREATE UNIQUE INDEX CONCURRENTLY "remote_sessions_client_issuer_id_key" ON "remote_sessions" ("remote_session_client_id", "user_session_issuer_id", "id");
-- Create "principal_remote_session_bindings" table
CREATE TABLE "principal_remote_session_bindings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "principal_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "remote_session_client_id" uuid NOT NULL,
  "remote_session_id" uuid NOT NULL,
  "issuer_attachment_scope" text NOT NULL,
  "client_attachment_scope" text NOT NULL,
  "grant_generation" bigint NOT NULL,
  "attached_by_subject_id" text NOT NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "principal_remote_session_bindings_client_issuer_fkey" FOREIGN KEY ("remote_session_client_id", "user_session_issuer_id") REFERENCES "remote_session_client_user_session_issuers" ("remote_session_client_id", "user_session_issuer_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "principal_remote_session_bindings_client_scope_fkey" FOREIGN KEY ("remote_session_client_id", "client_attachment_scope") REFERENCES "remote_session_clients" ("id", "attachment_scope") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "principal_remote_session_bindings_issuer_scope_fkey" FOREIGN KEY ("user_session_issuer_id", "issuer_attachment_scope") REFERENCES "user_session_issuers" ("id", "attachment_scope") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "principal_remote_session_bindings_principal_tenant_fkey" FOREIGN KEY ("organization_id", "principal_id") REFERENCES "agents" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "principal_remote_session_bindings_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "principal_remote_session_bindings_session_fkey" FOREIGN KEY ("remote_session_client_id", "user_session_issuer_id", "remote_session_id") REFERENCES "remote_sessions" ("remote_session_client_id", "user_session_issuer_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "principal_remote_session_bindings_client_scope_check" CHECK (((client_attachment_scope = ('project:'::text || (project_id)::text)) OR (client_attachment_scope = ('organization:'::text || organization_id))) OR (client_attachment_scope = 'global'::text)),
  CONSTRAINT "principal_remote_session_bindings_issuer_scope_check" CHECK ((issuer_attachment_scope = ('project:'::text || (project_id)::text)) OR (issuer_attachment_scope = ('organization:'::text || organization_id)))
);
-- Create index "principal_remote_session_bindings_active_key" to table: "principal_remote_session_bindings"
CREATE UNIQUE INDEX "principal_remote_session_bindings_active_key" ON "principal_remote_session_bindings" ("project_id", "principal_id", "user_session_issuer_id", "remote_session_client_id") WHERE (revoked_at IS NULL);
-- Create index "principal_remote_session_bindings_project_idx" to table: "principal_remote_session_bindings"
CREATE INDEX "principal_remote_session_bindings_project_idx" ON "principal_remote_session_bindings" ("project_id");
-- Create index "principal_remote_session_bindings_session_idx" to table: "principal_remote_session_bindings"
CREATE INDEX "principal_remote_session_bindings_session_idx" ON "principal_remote_session_bindings" ("remote_session_id");
-- Create trigger "principal_remote_session_bindings_scopes"
CREATE TRIGGER "principal_remote_session_bindings_scopes" BEFORE INSERT OR UPDATE ON "principal_remote_session_bindings" FOR EACH ROW EXECUTE FUNCTION "set_principal_remote_session_binding_scopes"();

-- atlas:txmode none

-- Create index "remote_session_clients_id_issuer_key" to table: "remote_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_clients_id_issuer_key" ON "remote_session_clients" ("id", "remote_session_issuer_id");
-- Create "remote_session_ema_bindings" table
CREATE TABLE "remote_session_ema_bindings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "remote_session_issuer_id" uuid NOT NULL,
  "resource" text NOT NULL,
  "remote_session_client_id" uuid NULL,
  "generation" bigint NOT NULL DEFAULT 1,
  "state" text NOT NULL DEFAULT 'configuration_required',
  "grant_source" text NOT NULL DEFAULT 'unknown',
  "requested_scopes" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "claim_id" uuid NULL,
  "claimed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_session_ema_bindings_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE CASCADE ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_remote_session_client_id_remot_fkey" FOREIGN KEY ("remote_session_client_id", "remote_session_issuer_id") REFERENCES "remote_session_clients" ("id", "remote_session_issuer_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "remote_session_ema_bindings_client_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_client_idx" ON "remote_session_ema_bindings" ("remote_session_client_id");
-- Create index "remote_session_ema_bindings_issuer_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_issuer_idx" ON "remote_session_ema_bindings" ("remote_session_issuer_id");
-- Create index "remote_session_ema_bindings_resource_key" to table: "remote_session_ema_bindings"
CREATE UNIQUE INDEX "remote_session_ema_bindings_resource_key" ON "remote_session_ema_bindings" ("project_id", "user_session_issuer_id", "remote_session_issuer_id", "resource");
-- Create index "remote_session_ema_bindings_user_issuer_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_user_issuer_idx" ON "remote_session_ema_bindings" ("user_session_issuer_id");
-- Create "guard_remote_session_ema_lifecycle" function
CREATE FUNCTION "guard_remote_session_ema_lifecycle" () RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM remote_session_ema_bindings b
    WHERE b.state <> 'unlinked' AND (
      (TG_TABLE_NAME = 'remote_session_clients' AND b.remote_session_client_id = OLD.id) OR
      (TG_TABLE_NAME = 'remote_session_issuers' AND b.remote_session_issuer_id = OLD.id) OR
      (TG_TABLE_NAME = 'user_session_issuers' AND b.user_session_issuer_id = OLD.id) OR
      (TG_TABLE_NAME = 'projects' AND b.project_id = OLD.id)
    )
  ) THEN
    RAISE EXCEPTION 'active identity-chaining binding must be explicitly unlinked before reconfiguration' USING ERRCODE = '23503';
  END IF;
  IF TG_OP = 'DELETE' THEN
    -- Once explicitly unlinked there is no live credential reference to retain.
    -- Remove tombstones before SET NULL FKs run against non-null owner columns.
    -- A stale completion still fails because its unique claim ID no longer exists.
    DELETE FROM remote_session_ema_bindings b WHERE b.state = 'unlinked' AND (
      (TG_TABLE_NAME = 'remote_session_clients' AND b.remote_session_client_id = OLD.id) OR
      (TG_TABLE_NAME = 'remote_session_issuers' AND b.remote_session_issuer_id = OLD.id) OR
      (TG_TABLE_NAME = 'user_session_issuers' AND b.user_session_issuer_id = OLD.id) OR
      (TG_TABLE_NAME = 'projects' AND b.project_id = OLD.id)
    );
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;
-- Create trigger "remote_session_ema_project_delete_guard"
CREATE TRIGGER "remote_session_ema_project_delete_guard" BEFORE DELETE ON "projects" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_project_update_guard"
CREATE TRIGGER "remote_session_ema_project_update_guard" BEFORE UPDATE ON "projects" FOR EACH ROW WHEN ((old.deleted_at IS DISTINCT FROM new.deleted_at) OR (old.organization_id IS DISTINCT FROM new.organization_id)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_client_delete_guard"
CREATE TRIGGER "remote_session_ema_client_delete_guard" BEFORE DELETE ON "remote_session_clients" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_client_update_guard"
CREATE TRIGGER "remote_session_ema_client_update_guard" BEFORE UPDATE ON "remote_session_clients" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.remote_session_issuer_id IS DISTINCT FROM new.remote_session_issuer_id) OR (old.deleted_at IS DISTINCT FROM new.deleted_at) OR (old.audience IS DISTINCT FROM new.audience) OR (old.client_id IS DISTINCT FROM new.client_id) OR (old.client_secret_encrypted IS DISTINCT FROM new.client_secret_encrypted) OR (old.token_endpoint_auth_method IS DISTINCT FROM new.token_endpoint_auth_method) OR (old.json_web_key_set_id IS DISTINCT FROM new.json_web_key_set_id) OR (old.scope IS DISTINCT FROM new.scope) OR (old.client_id_metadata_uri IS DISTINCT FROM new.client_id_metadata_uri) OR (old.token_endpoint_auth_audience_format IS DISTINCT FROM new.token_endpoint_auth_audience_format)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_issuer_delete_guard"
CREATE TRIGGER "remote_session_ema_issuer_delete_guard" BEFORE DELETE ON "remote_session_issuers" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_issuer_update_guard"
CREATE TRIGGER "remote_session_ema_issuer_update_guard" BEFORE UPDATE ON "remote_session_issuers" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.issuer IS DISTINCT FROM new.issuer) OR (old.deleted_at IS DISTINCT FROM new.deleted_at)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_user_issuer_delete_guard"
CREATE TRIGGER "remote_session_ema_user_issuer_delete_guard" BEFORE DELETE ON "user_session_issuers" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_user_issuer_update_guard"
CREATE TRIGGER "remote_session_ema_user_issuer_update_guard" BEFORE UPDATE ON "user_session_issuers" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.trusted_remote_session_issuer_id IS DISTINCT FROM new.trusted_remote_session_issuer_id) OR (old.deleted_at IS DISTINCT FROM new.deleted_at)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();

-- atlas:txmode none

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "grant_types" text[] NULL;
-- Create index "remote_session_clients_id_issuer_key" to table: "remote_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_clients_id_issuer_key" ON "remote_session_clients" ("id", "remote_session_issuer_id");
-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "authorization_grant_profiles_supported" text[] NOT NULL DEFAULT ARRAY[]::text[];
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
-- Create index "remote_session_ema_bindings_claim_key" to table: "remote_session_ema_bindings"
CREATE UNIQUE INDEX "remote_session_ema_bindings_claim_key" ON "remote_session_ema_bindings" ("claim_id") WHERE (claim_id IS NOT NULL);
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
CREATE TRIGGER "remote_session_ema_client_update_guard" BEFORE UPDATE ON "remote_session_clients" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.remote_session_issuer_id IS DISTINCT FROM new.remote_session_issuer_id) OR (old.deleted_at IS DISTINCT FROM new.deleted_at) OR (old.audience IS DISTINCT FROM new.audience) OR (old.client_id IS DISTINCT FROM new.client_id) OR (old.client_secret_encrypted IS DISTINCT FROM new.client_secret_encrypted) OR (old.client_secret_expires_at IS DISTINCT FROM new.client_secret_expires_at) OR (old.token_endpoint_auth_method IS DISTINCT FROM new.token_endpoint_auth_method) OR (old.json_web_key_set_id IS DISTINCT FROM new.json_web_key_set_id) OR (old.scope IS DISTINCT FROM new.scope) OR (old.client_id_metadata_uri IS DISTINCT FROM new.client_id_metadata_uri) OR (old.token_endpoint_auth_audience_format IS DISTINCT FROM new.token_endpoint_auth_audience_format)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_issuer_delete_guard"
CREATE TRIGGER "remote_session_ema_issuer_delete_guard" BEFORE DELETE ON "remote_session_issuers" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_issuer_update_guard"
CREATE TRIGGER "remote_session_ema_issuer_update_guard" BEFORE UPDATE ON "remote_session_issuers" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.issuer IS DISTINCT FROM new.issuer) OR (old.deleted_at IS DISTINCT FROM new.deleted_at) OR (old.authorization_endpoint IS DISTINCT FROM new.authorization_endpoint) OR (old.token_endpoint IS DISTINCT FROM new.token_endpoint) OR (old.revocation_endpoint IS DISTINCT FROM new.revocation_endpoint) OR (old.registration_endpoint IS DISTINCT FROM new.registration_endpoint) OR (old.jwks_uri IS DISTINCT FROM new.jwks_uri) OR (old.userinfo_endpoint IS DISTINCT FROM new.userinfo_endpoint) OR (old.introspection_endpoint IS DISTINCT FROM new.introspection_endpoint) OR (old.scope_override IS DISTINCT FROM new.scope_override) OR (old.resource_indicator_supported IS DISTINCT FROM new.resource_indicator_supported) OR (old.tunneled_mcp_server_id IS DISTINCT FROM new.tunneled_mcp_server_id)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_user_issuer_delete_guard"
CREATE TRIGGER "remote_session_ema_user_issuer_delete_guard" BEFORE DELETE ON "user_session_issuers" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_user_issuer_update_guard"
CREATE TRIGGER "remote_session_ema_user_issuer_update_guard" BEFORE UPDATE ON "user_session_issuers" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.trusted_remote_session_issuer_id IS DISTINCT FROM new.trusted_remote_session_issuer_id) OR (old.deleted_at IS DISTINCT FROM new.deleted_at)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create "validate_remote_session_ema_binding_scope" function
CREATE FUNCTION "validate_remote_session_ema_binding_scope" () RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  -- An unlinked tombstone may outlive parent reconfiguration. Preserve it (and
  -- the project's organization cascade), but revalidate any retarget or revival.
  IF TG_OP = 'UPDATE' THEN
    -- Unlink/revival changes the incarnation. Do not apply this rule to DCR
    -- completion: it publishes the selected claim within the same generation.
    IF NEW.generation < OLD.generation OR (
      (OLD.state = 'unlinked') IS DISTINCT FROM (NEW.state = 'unlinked')
      AND NEW.generation <> OLD.generation + 1
    ) THEN
      RAISE EXCEPTION 'identity-chaining unlink/revival requires the next generation'
        USING ERRCODE = '23514', CONSTRAINT = 'remote_session_ema_bindings_generation_check';
    END IF;
    IF OLD.state = 'unlinked' AND NEW.state = 'unlinked'
      AND OLD.project_id IS NOT DISTINCT FROM NEW.project_id
      AND OLD.user_session_issuer_id IS NOT DISTINCT FROM NEW.user_session_issuer_id
      AND OLD.remote_session_issuer_id IS NOT DISTINCT FROM NEW.remote_session_issuer_id
      AND OLD.remote_session_client_id IS NOT DISTINCT FROM NEW.remote_session_client_id THEN
      RETURN NEW;
    END IF;
  END IF;

  -- SHARE, not KEY SHARE: scope and soft-deletion changes need not alter a
  -- referenced key. Hold these locks until commit so a first binding cannot
  -- race a parent reconfiguration that observes no active bindings yet.
  PERFORM 1 FROM projects WHERE id = NEW.project_id
    AND organization_id = NEW.organization_id AND deleted IS FALSE FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'identity-chaining project scope mismatch'
      USING ERRCODE = '23503', CONSTRAINT = 'remote_session_ema_bindings_project_scope_fkey';
  END IF;
  -- The user issuer trusts an upstream IdP; this binding selects a downstream
  -- resource AS. Their issuer IDs need not match. Validate each parent scope.
  PERFORM 1 FROM user_session_issuers WHERE id = NEW.user_session_issuer_id AND deleted IS FALSE
    AND (project_id = NEW.project_id OR (project_id IS NULL AND organization_id = NEW.organization_id)) FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'identity-chaining user issuer scope mismatch'
      USING ERRCODE = '23503', CONSTRAINT = 'remote_session_ema_bindings_user_issuer_scope_fkey';
  END IF;
  PERFORM 1 FROM remote_session_issuers WHERE id = NEW.remote_session_issuer_id AND deleted IS FALSE
    AND (project_id = NEW.project_id OR (project_id IS NULL AND (organization_id = NEW.organization_id OR organization_id IS NULL))) FOR SHARE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'identity-chaining remote issuer scope mismatch'
      USING ERRCODE = '23503', CONSTRAINT = 'remote_session_ema_bindings_remote_issuer_scope_fkey';
  END IF;
  IF NEW.remote_session_client_id IS NOT NULL THEN
    -- Global clients are platform-owned and cannot be selected by tenant preparation.
    PERFORM 1 FROM remote_session_clients WHERE id = NEW.remote_session_client_id AND deleted IS FALSE
      AND (project_id = NEW.project_id OR (project_id IS NULL AND organization_id = NEW.organization_id)) FOR SHARE;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'identity-chaining client scope mismatch'
        USING ERRCODE = '23503', CONSTRAINT = 'remote_session_ema_bindings_client_scope_fkey';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;
-- Create trigger "remote_session_ema_binding_scope_guard"
CREATE TRIGGER "remote_session_ema_binding_scope_guard" BEFORE INSERT OR UPDATE ON "remote_session_ema_bindings" FOR EACH ROW EXECUTE FUNCTION "validate_remote_session_ema_binding_scope"();

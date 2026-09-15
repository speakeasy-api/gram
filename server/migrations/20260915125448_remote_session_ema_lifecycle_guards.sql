-- Create "guard_remote_session_ema_lifecycle" function
CREATE FUNCTION "guard_remote_session_ema_lifecycle" () RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM remote_session_ema_bindings b
    WHERE b.state <> 'unlinked' AND (
      (TG_TABLE_NAME = 'remote_session_clients' AND b.remote_session_client_id = OLD.id) OR
      (TG_TABLE_NAME = 'remote_session_issuers' AND b.remote_session_issuer_id = OLD.id) OR
      (TG_TABLE_NAME = 'user_session_issuers' AND b.user_session_issuer_id = OLD.id)
    )
  ) THEN
    RAISE EXCEPTION 'active identity-chaining binding must be explicitly unlinked before reconfiguration' USING ERRCODE = '23503';
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END;
$$;
-- Create trigger "remote_session_ema_client_delete_guard"
CREATE TRIGGER "remote_session_ema_client_delete_guard" BEFORE DELETE ON "remote_session_clients" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_client_update_guard"
CREATE TRIGGER "remote_session_ema_client_update_guard" BEFORE UPDATE ON "remote_session_clients" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.remote_session_issuer_id IS DISTINCT FROM new.remote_session_issuer_id) OR (old.deleted_at IS DISTINCT FROM new.deleted_at) OR (old.client_id IS DISTINCT FROM new.client_id) OR (old.client_secret_encrypted IS DISTINCT FROM new.client_secret_encrypted) OR (old.token_endpoint_auth_method IS DISTINCT FROM new.token_endpoint_auth_method) OR (old.json_web_key_set_id IS DISTINCT FROM new.json_web_key_set_id) OR (old.scope IS DISTINCT FROM new.scope) OR (old.client_id_metadata_uri IS DISTINCT FROM new.client_id_metadata_uri) OR (old.token_endpoint_auth_audience_format IS DISTINCT FROM new.token_endpoint_auth_audience_format)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_issuer_delete_guard"
CREATE TRIGGER "remote_session_ema_issuer_delete_guard" BEFORE DELETE ON "remote_session_issuers" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_issuer_update_guard"
CREATE TRIGGER "remote_session_ema_issuer_update_guard" BEFORE UPDATE ON "remote_session_issuers" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.issuer IS DISTINCT FROM new.issuer) OR (old.deleted_at IS DISTINCT FROM new.deleted_at)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_user_issuer_delete_guard"
CREATE TRIGGER "remote_session_ema_user_issuer_delete_guard" BEFORE DELETE ON "user_session_issuers" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Create trigger "remote_session_ema_user_issuer_update_guard"
CREATE TRIGGER "remote_session_ema_user_issuer_update_guard" BEFORE UPDATE ON "user_session_issuers" FOR EACH ROW WHEN ((old.project_id IS DISTINCT FROM new.project_id) OR (old.organization_id IS DISTINCT FROM new.organization_id) OR (old.trusted_remote_session_issuer_id IS DISTINCT FROM new.trusted_remote_session_issuer_id) OR (old.deleted_at IS DISTINCT FROM new.deleted_at)) EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();

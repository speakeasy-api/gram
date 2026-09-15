-- Create trigger "remote_session_ema_project_delete_guard"
CREATE TRIGGER "remote_session_ema_project_delete_guard" BEFORE DELETE ON "projects" FOR EACH ROW EXECUTE FUNCTION "guard_remote_session_ema_lifecycle"();
-- Modify "guard_remote_session_ema_lifecycle" function
CREATE OR REPLACE FUNCTION "guard_remote_session_ema_lifecycle" () RETURNS trigger LANGUAGE plpgsql AS $$
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

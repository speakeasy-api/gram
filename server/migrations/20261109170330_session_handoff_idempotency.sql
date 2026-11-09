-- atlas:txmode none

-- Create "session_handoff_upload_intents" table
CREATE TABLE "session_handoff_upload_intents" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "actor_user_id" text NULL,
  "object_path" text NOT NULL,
  "blob_url" text NOT NULL DEFAULT '',
  "state" text NOT NULL DEFAULT 'pending',
  "last_error" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "session_handoff_upload_intents_actor_check" CHECK ((actor_user_id IS NULL) OR (actor_user_id <> ''::text))
);
-- Create index "session_handoff_upload_intents_blob_url_key" to table: "session_handoff_upload_intents"
CREATE UNIQUE INDEX "session_handoff_upload_intents_blob_url_key" ON "session_handoff_upload_intents" ("blob_url") WHERE (blob_url <> ''::text);
-- Create index "session_handoff_upload_intents_stale_idx" to table: "session_handoff_upload_intents"
CREATE INDEX "session_handoff_upload_intents_stale_idx" ON "session_handoff_upload_intents" ("created_at", "id") WHERE (state = ANY (ARRAY['pending'::text, 'cleaning'::text, 'linked'::text]));
-- Set comment to column: "actor_user_id" on table: "session_handoff_upload_intents"
COMMENT ON COLUMN "session_handoff_upload_intents"."actor_user_id" IS '@access: confidential';
-- Set comment to column: "object_path" on table: "session_handoff_upload_intents"
COMMENT ON COLUMN "session_handoff_upload_intents"."object_path" IS '@access: secret-restricted';
-- Set comment to column: "blob_url" on table: "session_handoff_upload_intents"
COMMENT ON COLUMN "session_handoff_upload_intents"."blob_url" IS '@access: secret-restricted';
-- Create "register_session_handoff_blob" function
CREATE FUNCTION "register_session_handoff_blob" () RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.blob_url <> '' AND NEW.project_id IS NOT NULL
     AND (TG_OP = 'INSERT' OR OLD.blob_url IS DISTINCT FROM NEW.blob_url) THEN
    INSERT INTO session_handoff_upload_intents (
      project_id, organization_id, actor_user_id, object_path, blob_url, state
    ) VALUES (
      NEW.project_id, NEW.organization_id, NULLIF(to_jsonb(NEW)->>'actor_user_id', ''), '', NEW.blob_url, 'linked'
    )
    ON CONFLICT (blob_url) WHERE blob_url <> '' DO UPDATE
    SET project_id = EXCLUDED.project_id,
        organization_id = EXCLUDED.organization_id,
        actor_user_id = COALESCE(session_handoff_upload_intents.actor_user_id, EXCLUDED.actor_user_id),
        state = 'linked',
        last_error = NULL,
        updated_at = clock_timestamp();
  END IF;
  RETURN NEW;
END;
$$;
-- Create trigger "session_handoff_links_register_blob"
CREATE TRIGGER "session_handoff_links_register_blob" AFTER INSERT OR UPDATE OF "blob_url" ON "session_handoff_links" FOR EACH ROW EXECUTE FUNCTION "register_session_handoff_blob"();
-- Backfill durable cleanup pointers before project deletion can null link scope.
-- Values stay inside Postgres; the migration emits no per-row tokens or URLs.
INSERT INTO "session_handoff_upload_intents" (
  "project_id", "organization_id", "actor_user_id", "object_path", "blob_url", "state"
)
SELECT "project_id", "organization_id", NULL, '', "blob_url", 'linked'
FROM "session_handoff_links"
WHERE "project_id" IS NOT NULL AND "blob_url" <> ''
ON CONFLICT ("blob_url") WHERE "blob_url" <> '' DO UPDATE
SET "project_id" = EXCLUDED."project_id",
    "organization_id" = EXCLUDED."organization_id",
    "state" = 'linked',
    "last_error" = NULL,
    "updated_at" = clock_timestamp();
-- Modify "session_handoff_links" table
ALTER TABLE "session_handoff_links" DROP CONSTRAINT "session_handoff_links_organization_project_fkey", ADD CONSTRAINT "session_handoff_links_actor_user_id_check" CHECK ((actor_user_id IS NULL) OR (actor_user_id <> ''::text)) NOT VALID, ADD CONSTRAINT "session_handoff_links_idempotency_input_hash_check" CHECK ((idempotency_input_hash IS NULL) OR (idempotency_input_hash ~ '^[0-9a-f]{64}$'::text)) NOT VALID, ADD CONSTRAINT "session_handoff_links_idempotency_key_check" CHECK ((idempotency_key IS NULL) OR ((idempotency_key = btrim(idempotency_key)) AND ((octet_length(idempotency_key) >= 1) AND (octet_length(idempotency_key) <= 128)))) NOT VALID, ADD CONSTRAINT "session_handoff_links_idempotency_pair_check" CHECK (((idempotency_key IS NULL) = (idempotency_input_hash IS NULL)) AND ((idempotency_key IS NULL) = (idempotency_operation IS NULL))) NOT VALID, ALTER COLUMN "project_id" DROP NOT NULL, ADD COLUMN "actor_user_id" text NULL, ADD COLUMN "idempotency_operation" text NULL, ADD COLUMN "idempotency_key" text NULL, ADD COLUMN "idempotency_input_hash" text NULL, ADD COLUMN "blob_cleanup_state" text NOT NULL DEFAULT 'available', ADD COLUMN "blob_cleanup_error" text NULL, ADD COLUMN "blob_cleanup_attempted_at" timestamptz NULL, ADD CONSTRAINT "session_handoff_links_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("project_id") NOT VALID;
ALTER TABLE "session_handoff_links" VALIDATE CONSTRAINT "session_handoff_links_actor_user_id_check";
ALTER TABLE "session_handoff_links" VALIDATE CONSTRAINT "session_handoff_links_idempotency_input_hash_check";
ALTER TABLE "session_handoff_links" VALIDATE CONSTRAINT "session_handoff_links_idempotency_key_check";
ALTER TABLE "session_handoff_links" VALIDATE CONSTRAINT "session_handoff_links_idempotency_pair_check";
ALTER TABLE "session_handoff_links" VALIDATE CONSTRAINT "session_handoff_links_organization_project_fkey";

-- Create index "session_handoff_links_device_idempotency_key" to table: "session_handoff_links"
CREATE UNIQUE INDEX CONCURRENTLY "session_handoff_links_device_idempotency_key" ON "session_handoff_links" ("organization_id", "project_id", "actor_user_id", "idempotency_operation", "idempotency_key") WHERE ((idempotency_key IS NOT NULL) AND (idempotency_operation = 'device_create'::text));
-- Create index "session_handoff_links_platform_idempotency_key" to table: "session_handoff_links"
CREATE UNIQUE INDEX CONCURRENTLY "session_handoff_links_platform_idempotency_key" ON "session_handoff_links" ("organization_id", "actor_user_id", "idempotency_operation", "idempotency_key") WHERE ((idempotency_key IS NOT NULL) AND (idempotency_operation = 'platform_create'::text));
-- Set comment to column: "actor_user_id" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."actor_user_id" IS '@access: confidential';
-- Set comment to column: "idempotency_operation" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."idempotency_operation" IS '@access: confidential';
-- Set comment to column: "idempotency_key" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."idempotency_key" IS '@access: confidential';
-- Set comment to column: "idempotency_input_hash" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."idempotency_input_hash" IS '@access: confidential';
-- Set comment to column: "blob_cleanup_state" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."blob_cleanup_state" IS '@access: confidential';
-- Set comment to column: "blob_cleanup_error" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."blob_cleanup_error" IS '@access: confidential';
-- Set comment to column: "blob_cleanup_attempted_at" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."blob_cleanup_attempted_at" IS '@access: confidential';
